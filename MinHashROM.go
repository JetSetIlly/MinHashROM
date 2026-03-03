package main

import (
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"crypto/sha1"
	_ "embed"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/dgryski/go-farm"
	"github.com/dgryski/go-spooky"
	"github.com/shawnohare/go-minhash"
)

const (
	supportedVersion = 1
	defaultChunkSize = 64
	readBlockSize    = 256
	hashSize         = 8
)

const (
	sigCompressedDB = "gzipMinHashRomDB"
)

type minHashRom struct {
	programName string
	mode        string
	args        []string
}

func main() {
	var mhr minHashRom
	mhr.programName = filepath.Base(os.Args[0])

	var err error

	// use flag set to provide the --help flag for top level command line.
	// that's all we want it to do
	flgs := flag.NewFlagSet("minhashrom", flag.ContinueOnError)

	// setting flag output to the nilWriter because we need to control how
	// unrecognised arguments are displayed
	flgs.SetOutput(&nilWriter{})

	// first argument is always the invoked program name
	args := os.Args[1:]

	// parse arguments. if the help flag has been used then print out the
	// execution modes summary and return
	err = flgs.Parse(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Println("Execution Modes: MATCH, SEARCH, CREATE")
			return
		}
	} else {
		args = flgs.Args()
	}

	if len(args) != 0 {
		mhr.mode = strings.ToUpper(args[0])
	}

	switch mhr.mode {
	case "CREATE":
		mhr.args = os.Args[2:]
		err = mhr.create()
	case "MATCH":
		mhr.args = os.Args[2:]
		err = mhr.match()
	case "SEARCH":
		mhr.args = os.Args[2:]
		err = mhr.search()
	default:
		mhr.mode = "MATCH"
		mhr.args = os.Args[1:]
		err = mhr.match()
	}

	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}

func (mhr minHashRom) create() error {
	var chunkSize int
	var dbFile string
	var compress bool

	flgs := flag.NewFlagSet(mhr.mode, flag.ExitOnError)
	flgs.IntVar(&chunkSize, "c", defaultChunkSize, "chunk size")
	flgs.StringVar(&dbFile, "db", "minhash.db", "name of minhash database file")
	flgs.BoolVar(&compress, "compress", true, "compress database file")

	flgs.Usage = func() {
		fmt.Printf("usage: %s %s [ROM directory]\n", mhr.programName, mhr.mode)
		flgs.PrintDefaults()
	}

	flgs.Parse(mhr.args)

	// parameter check
	if chunkSize < 8 {
		return fmt.Errorf("chunk size of less than 8 is pointless")
	}
	if chunkSize > 2048 {
		return fmt.Errorf("chunk size of more than 2048 is pointless")
	}
	if 4096%chunkSize != 0 {
		return fmt.Errorf("chunk size must divide equally into 4096")
	}

	args := flgs.Args()
	if len(args) != 1 {
		flgs.Usage()
		return nil
	}
	roms := args[0]

	f, err := os.Create(dbFile)
	if err != nil {
		return fmt.Errorf("error creating database: %w", err)
	}

	// compress data if requested
	var db io.WriteCloser

	if compress {
		f.WriteString(sigCompressedDB)
		db = gzip.NewWriter(f)
	} else {
		db = f
	}

	defer db.Close()

	// write header
	db.Write([]byte{
		byte(supportedVersion >> 8),
		byte(supportedVersion),
	})
	db.Write([]byte{
		byte(chunkSize >> 8),
		byte(chunkSize),
	})

	// progress
	fmt.Printf("creating db file %s\n", dbFile)
	fmt.Printf("chunk size %d\n", chunkSize)

	var entryCount int

	createFunc := func(path string, mw *minhash.MinHash) error {
		// write path to dbfile making it's not too long
		path = filepath.Base(path)
		path = path[:min(readBlockSize-1, len(path))]
		db.Write([]byte(path))
		db.Write([]byte{0x00})

		var buffer [8]byte

		for _, sig := range mw.Signature() {
			binary.LittleEndian.PutUint64(buffer[:], sig)
			_, err := db.Write(buffer[:])
			if err != nil {
				return fmt.Errorf("error creating database: %w", err)
			}
		}

		entryCount++
		return nil
	}

	err = mhr.walkRomDirectory(roms, chunkSize, createFunc)
	if err != nil {
		return err
	}

	if entryCount == 1 {
		fmt.Printf("%d entry written\n", entryCount)
	} else {
		fmt.Printf("%d entries written\n", entryCount)
	}

	return nil
}

type Process struct {
	dbData    []byte
	numSigs   int
	chunkSize int

	verbose     bool
	search      bool
	sensitivity float64
}

func (p *Process) openDB(dbFile string) error {
	dbData, err := os.ReadFile(dbFile)
	if err != nil {
		return fmt.Errorf("cannot open database: %s", dbFile)
	}

	var buffer [readBlockSize]byte

	// at this point the db reader can be compressed or uncompressed data
	db := bytes.NewReader(dbData)

	// check first few bytes for compressed data signature
	sigLen := len(sigCompressedDB)
	n, err := db.Read(buffer[:sigLen])
	if err != nil {
		return fmt.Errorf("cannot open database: %s: %w", dbFile, err)
	}
	if n != len(sigCompressedDB) {
		return fmt.Errorf("cannot open database: %s: file too short", dbFile)
	}

	// if database file has the compressed db signature then replace db with the uncompressed data
	if string(buffer[:sigLen]) == sigCompressedDB {
		z, err := gzip.NewReader(db)
		if err != nil {
			return fmt.Errorf("cannot open database: %s: %w", dbFile, err)
		}

		dbData, err = io.ReadAll(z)
		if err != nil {
			return fmt.Errorf("cannot open database: %s: %w", dbFile, err)
		}

		db = bytes.NewReader(dbData)
	}

	// make sure we're at beginning of data
	db.Seek(0, io.SeekStart)

	// copy of uncompressed dbData. we don't store the reader because we want to be able to read the
	// data concurrently and we need a new reader for each goroutine
	p.dbData = dbData

	// at this point db is definitely uncompressed data

	// read database header
	n, err = db.Read(buffer[:4])
	if err != nil {
		return fmt.Errorf("error reading database: %s %w", dbFile, err)
	}
	if n != 4 {
		return fmt.Errorf("invalid database file: %s: no header found", dbFile)
	}

	// check for validity
	if supportedVersion != (int(buffer[0])<<8)|int(buffer[1]) {
		return fmt.Errorf("invalid database file: %s: unsupported version", dbFile)
	}

	p.chunkSize = (int(buffer[2]) << 8) | int(buffer[3])
	if p.chunkSize <= 0 || 4096%p.chunkSize != 0 {
		return fmt.Errorf("invalid database file: %s: invalid chunk size", dbFile)
	}

	// number of signatures per rom
	p.numSigs = 4096 / p.chunkSize

	if p.verbose {
		fmt.Printf("matching with db file %s with chunk size %d\n", dbFile, p.chunkSize)
	}

	return nil
}

// using a copy of the Process instance. the pointer to dbData is fine to access concurrently
// because we're only ever reading it
func (p Process) run(f []uint8) (strings.Builder, error) {
	// prepare minhash for comparison rom
	cmp := minhash.New(spooky.Hash64, farm.Hash64, 4096/p.chunkSize)
	for i := 0; i < len(f); i += p.chunkSize {
		cmp.Push(f[i : i+p.chunkSize-1])
	}

	var matches strings.Builder

	var done bool
	var matchCount int
	var entryCount int

	if p.verbose && !p.search {
		defer func() {
			if matchCount == 1 {
				fmt.Fprintf(&matches, "%d entry matched\n", matchCount)
			} else {
				fmt.Fprintf(&matches, "%d entries matched\n", matchCount)
			}
			if entryCount == 1 {
				fmt.Fprintf(&matches, "%d entry checked\n", entryCount)
			} else {
				fmt.Fprintf(&matches, "%d entries checked\n", entryCount)
			}
		}()
	}

	var buffer [readBlockSize]byte
	db := bytes.NewReader(p.dbData)
	db.Seek(0, io.SeekStart)

	for !done {
		// read rom name from database
		n, err := db.Read(buffer[:])
		if err != nil {
			if err == io.EOF {
				return matches, nil
			}
			return matches, fmt.Errorf("error reading database: %w", err)
		}

		// the amount of data read was less than the block size, this means this
		// will be the last iteration
		done = done || n < readBlockSize

		n = bytes.IndexByte(buffer[:n], 0x00)
		if n == -1 {
			return matches, fmt.Errorf("invalid database file: unexpected end of file")
		}
		rom := string(buffer[:n])

		// position file at start of hashes for the file
		_, err = db.Seek(-int64(len(buffer)-n-1), io.SeekCurrent)
		if err != nil {
			return matches, fmt.Errorf("error reading database: %w", err)
		}

		// read all signatures from rom
		var sigs []uint64

		for range p.numSigs {
			n, err = db.Read(buffer[:hashSize])
			if err != nil {
				return matches, fmt.Errorf("error reading database: %w", err)
			}
			if n != hashSize {
				return matches, fmt.Errorf("invalid database file: unexpected end of file")
			}

			sig := binary.LittleEndian.Uint64(buffer[:])
			sigs = append(sigs, sig)
		}

		mw := minhash.New(spooky.Hash64, farm.Hash64, 4096/p.chunkSize)
		mw.SetSignature(sigs)

		s := minhash.Similarity(mw, cmp) * 100
		if s >= p.sensitivity {
			fmt.Fprintf(&matches, "%6.02f%%   %s \n", s, rom)
			matchCount++
		}

		entryCount++
	}

	return matches, nil
}

func (mhr minHashRom) search() error {
	var p Process

	var dbFile string
	var numParallel int
	var resume int

	flgs := flag.NewFlagSet(mhr.mode, flag.ExitOnError)

	flgs.Float64Var(&p.sensitivity, "s", 80.0, "match sensitivity")
	flgs.StringVar(&dbFile, "db", "minhash.db", "name of minhash database file")
	flgs.IntVar(&numParallel, "n", runtime.NumCPU(), "number of parallel searches")
	flgs.IntVar(&resume, "r", 0, "byte offset to start searching from")

	flgs.Usage = func() {
		fmt.Printf("usage: %s %s [comparison ROM]\n", mhr.programName, mhr.mode)
		flgs.PrintDefaults()
	}

	flgs.Parse(mhr.args)

	args := flgs.Args()
	if len(args) != 1 {
		flgs.Usage()
		return nil
	}

	// load data file to be searched
	path, err := filepath.EvalSymlinks(args[0])
	if err != nil {
		return fmt.Errorf("ROM does not exist: %s", args[0])
	}
	var f []byte
	f, err = os.ReadFile(path)
	if err != nil {
		return err
	}

	// minhash database
	err = p.openDB(dbFile)
	if err != nil {
		return err
	}

	// keep number of goroutines under control
	var wg sync.WaitGroup

	counter := make(chan bool, numParallel)
	for range cap(counter) {
		counter <- true
	}

	// errors returned from goroutine over fatalErr channel
	fatalErr := make(chan error)

	const searchBlockSize = 4096

	// catch interrupts
	intChan := make(chan os.Signal, 1)
	signal.Notify(intChan, os.Interrupt)

	// wait for all outstanding goroutines to complete
	defer wg.Wait()

	for i := range f[resume : len(f)-searchBlockSize-1] {
		fmt.Printf("Progress: %.2f%%   \r", 100*float64(i+resume)/float64(len(f)))

		// do no proceed until a count token is available
		<-counter

		wg.Go(func() {
			// give back counter when goroutine finishes. this allows another goroutine to start
			defer func() {
				counter <- true
			}()

			// run search process and print out results if ok
			matches, err := p.run(f[i : i+searchBlockSize])
			if err != nil {
				fatalErr <- err
				return
			}
			if matches.Len() > 0 {
				fmt.Print(matches.String())
				fmt.Printf("[Slice at %d to %d]\n", i, i+searchBlockSize)
			}
		})

		select {
		case err := <-fatalErr:
			return err
		case <-intChan:
			fmt.Printf("\n-----\nUser Interrupt: resume at byte %d\n", i+resume)
			return nil
		default:
		}
	}

	return nil
}

func (mhr minHashRom) match() error {
	var p Process

	var dbFile string
	var verbose bool

	flgs := flag.NewFlagSet(mhr.mode, flag.ExitOnError)

	flgs.Float64Var(&p.sensitivity, "s", 80.0, "match sensitivity")
	flgs.StringVar(&dbFile, "db", "minhash.db", "name of minhash database file")
	flgs.BoolVar(&verbose, "v", false, "verbose output")

	flgs.Usage = func() {
		fmt.Printf("usage: %s %s [comparison ROM]\n", mhr.programName, mhr.mode)
		flgs.PrintDefaults()
	}

	flgs.Parse(mhr.args)

	args := flgs.Args()
	if len(args) != 1 {
		flgs.Usage()
		return nil
	}

	// open rom file to check
	path, err := filepath.EvalSymlinks(args[0])
	if err != nil {
		return fmt.Errorf("ROM does not exist: %s", args[0])
	}
	var f []byte
	if verbose {
		f, err = loadROM(path, os.Stdout)
	} else {
		f, err = loadROM(path, nil)
	}
	if err != nil {
		return err
	}

	// minhash database
	err = p.openDB(dbFile)
	if err != nil {
		return err
	}

	// run match process and print out results if ok
	matches, err := p.run(f)
	if err != nil {
		return err
	}
	if matches.Len() > 0 {
		fmt.Print(matches.String())
	}
	return nil
}

func (mhr minHashRom) walkRomDirectory(roms string, chunkSize int, hook func(string, *minhash.MinHash) error) error {
	err := filepath.Walk(roms, func(path string, info fs.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}

		// ignore files that are very big
		if info.Size() >= 65536 {
			return nil
		}

		f, err := loadROM(path, nil)
		if err != nil {
			if errors.Is(err, unsupportedFileSize) {
				return nil
			}
			return err
		}

		mw := minhash.New(spooky.Hash64, farm.Hash64, 4096/chunkSize)
		for i := 0; i < len(f); i += chunkSize {
			mw.Push(f[i : i+chunkSize-1])
		}

		err = hook(path, mw)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("error reading ROMs: %w", err)
	}

	return nil
}

var unsupportedFileSize = errors.New("unsupported file size")

func loadROM(path string, verbose io.Writer) ([]byte, error) {
	f, err := os.ReadFile(path)
	if err != nil {
		return []byte{}, fmt.Errorf("error opening %s", path)
	}

	if verbose != nil {
		fmt.Fprintf(verbose, "file: %s\n", path)
		fmt.Fprintf(verbose, " md5: %x\n", md5.Sum(f))
		fmt.Fprintf(verbose, "sha1: %x\n", sha1.Sum(f))
		fmt.Fprintln(verbose, "")
	}

	if len(f) == 2048 {
		// 2k files need to be doubled up to 4096 bytes
		return append(f, f...), nil
	} else if len(f) >= 4096 {
		// cap files to 4096 bytes
		return f[:4096], nil
	} else {
		// files shorter than 4096 but not 2048 are not supported
		return []byte{}, fmt.Errorf("%w: file size: %d bytes", unsupportedFileSize, len(f))
	}
}

// nilWriter is used by the top level FlagSet. we want to be able to provide arguments to the
// default mode without complaining from the top level
type nilWriter struct{}

func (*nilWriter) Write(p []byte) (n int, err error) {
	return 0, nil
}
