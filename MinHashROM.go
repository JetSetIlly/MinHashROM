package main

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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

type minHashRom struct {
	programName string
	mode        string
	args        []string
}

func main() {
	var mhr minHashRom
	mhr.programName = filepath.Base(os.Args[0])

	var err error

	// find mode
	if len(os.Args) > 1 {
		mhr.mode = strings.ToUpper(os.Args[1])
	}

	switch mhr.mode {
	case "CREATE":
		mhr.args = os.Args[2:]
		err = mhr.create()
	case "MATCH":
		mhr.args = os.Args[2:]
		err = mhr.match()
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

	flgs := flag.NewFlagSet(mhr.mode, flag.ExitOnError)
	flgs.IntVar(&chunkSize, "c", defaultChunkSize, "chunk size")
	flgs.StringVar(&dbFile, "db", "minhash.db", "name of minhash database file")

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

	db, err := os.Create(dbFile)
	if err != nil {
		return fmt.Errorf("error creating database: %w", err)
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

func (mhr minHashRom) match() error {
	var sensitivity float64
	var dbFile string
	var verbose bool

	flgs := flag.NewFlagSet(mhr.mode, flag.ExitOnError)

	flgs.Float64Var(&sensitivity, "s", 80.0, "match sensitivity")
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

	// load comparison rom
	path, err := filepath.EvalSymlinks(args[0])
	if err != nil {
		return fmt.Errorf("ROM does not exist: %s", args[0])
	}

	f, err := loadROM(path)
	if err != nil {
		return err
	}

	// open database
	db, err := os.Open(dbFile)
	if err != nil {
		return fmt.Errorf("cannot open database: %s", dbFile)
	}
	defer db.Close()

	var buffer [readBlockSize]byte

	// read database header
	n, err := db.Read(buffer[:4])
	if err != nil {
		return fmt.Errorf("error reading database: %w", err)
	}
	if n != 4 {
		return fmt.Errorf("invalid database file: no header found")
	}

	if supportedVersion != (int(buffer[0])<<8)|int(buffer[1]) {
		return fmt.Errorf("invalid database file: unsupported version")
	}

	chunkSize := (int(buffer[2]) << 8) | int(buffer[3])
	if chunkSize <= 0 || 4096%chunkSize != 0 {
		return fmt.Errorf("invalid database file: invalid chunk size")
	}

	// number of signatures per rom
	numSigs := 4096 / chunkSize

	// progress
	if verbose {
		fmt.Printf("matching with db file %s\nwith chunk size %d\n", dbFile, chunkSize)
		fmt.Println("------")
	}

	// prepare minhash for comparison rom
	cmp := minhash.New(spooky.Hash64, farm.Hash64, 4096/chunkSize)
	for i := 0; i < len(f); i += chunkSize {
		cmp.Push(f[i : i+chunkSize-1])
	}

	var done bool
	var matchCount int
	var entryCount int

	if verbose {
		defer func() {
			fmt.Println("------")
			if matchCount == 1 {
				fmt.Printf("%d entry matched\n", matchCount)
			} else {
				fmt.Printf("%d entries matched\n", matchCount)
			}
			if entryCount == 1 {
				fmt.Printf("%d entry checked\n", entryCount)
			} else {
				fmt.Printf("%d entries checked\n", entryCount)
			}
		}()
	}

	for !done {
		// read rom name from database
		n, err = db.Read(buffer[:])
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("error reading database: %w", err)
		}

		// the amount of data read was less than the block size, this means this
		// will be the last iteration
		done = done || n < readBlockSize

		n = bytes.IndexByte(buffer[:n], 0x00)
		if n == -1 {
			return fmt.Errorf("invalid database file: unexpected end of file")
		}
		rom := string(buffer[:n])

		// position file at start of hashes for the file
		_, err = db.Seek(-int64(len(buffer)-n-1), io.SeekCurrent)
		if err != nil {
			return fmt.Errorf("error reading database: %w", err)
		}

		// read all signatures from rom
		var sigs []uint64

		for i := 0; i < numSigs; i++ {
			n, err = db.Read(buffer[:hashSize])
			if err != nil {
				return fmt.Errorf("error reading database: %w", err)
			}
			if n != hashSize {
				return fmt.Errorf("invalid database file: unexpected end of file")
			}

			sig := binary.LittleEndian.Uint64(buffer[:])
			sigs = append(sigs, sig)
		}

		mw := minhash.New(spooky.Hash64, farm.Hash64, 4096/chunkSize)
		mw.SetSignature(sigs)

		s := minhash.Similarity(mw, cmp) * 100
		if s >= sensitivity {
			fmt.Printf("%6.02f%%   %s \n", s, rom)
			matchCount++
		}

		entryCount++
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

		f, err := loadROM(path)
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

func loadROM(path string) ([]byte, error) {
	f, err := os.ReadFile(path)
	if err != nil {
		return []byte{}, fmt.Errorf("error opening %s", path)
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
