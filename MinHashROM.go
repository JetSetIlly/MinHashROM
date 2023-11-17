package main

import (
	_ "embed"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/dgryski/go-farm"
	"github.com/dgryski/go-spooky"
	"github.com/shawnohare/go-minhash"
)

const (
	romSize   = 4096
	chunkSize = 64
)

func main() {
	log.SetFlags(0)
	var err error

	if len(os.Args) != 3 {
		programName := filepath.Base(os.Args[0])
		log.Fatalf("usage: %s <rom directory> <rom to compare>\n", programName)
	}

	romDir, err := filepath.EvalSymlinks(os.Args[1])
	if err != nil {
		log.Fatalf("rom directory: %v", err)
		return
	}

	cmpFile, err := filepath.EvalSymlinks(os.Args[2])
	if err != nil {
		log.Fatalf("rom to compare: %v", err)
		return
	}

	f, err := os.ReadFile(cmpFile)
	if err != nil {
		log.Fatalf("error reading rom: %s: %v", cmpFile, err)
	}
	if len(f) != romSize {
		log.Fatalf("only ROMs of %d bytes are supported for now: %s: %v", romSize, cmpFile, err)
	}

	cmpmw := minhash.New(spooky.Hash64, farm.Hash64, romSize/chunkSize)
	for i := 0; i < len(f); i += chunkSize {
		cmpmw.Push(f[i : i+chunkSize-1])
	}

	var roms []string

	err = filepath.Walk(romDir, func(romFile string, info fs.FileInfo, err error) error {
		if !info.IsDir() && info.Size() == romSize {
			roms = append(roms, romFile)
		}
		return nil
	})
	if err != nil {
		log.Fatalf("error reading ROMs: %v", err)
		return
	}

	var highest float64

	for _, romFile := range roms {
		f, err := os.ReadFile(romFile)
		if err != nil {
			log.Fatalf("error reading rom: %s: %v", cmpFile, err)
		}
		rommw := minhash.New(spooky.Hash64, farm.Hash64, romSize/chunkSize)
		for i := 0; i < len(f); i += chunkSize {
			rommw.Push(f[i : i+chunkSize-1])
		}

		s := minhash.Similarity(rommw, cmpmw) * 100
		if s > 0 && s >= highest {
			fmt.Printf("%6.02f%%   %s \n", s, filepath.Base(romFile))
			highest = s
		}
	}

}
