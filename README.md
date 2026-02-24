# MinHashROM

Fuzzy matching of `Atari 2600` ROM files with a database of precomputed hashes. This is useful for finding the identity of unknown ROM files, particularly those
that may differ by a few bytes. It can also be useful for finding similar ROMs. For example, prototype versions of a game or versions that include custom graphics.

For the fuzzy matching to work a database needs to be created from a collection of existing ROM files. A good ROM collection to start with is the 
[Atari 2600 VCS ROM Collection](http://www.atarimania.com/rom_collection_archive_atari_2600_roms.html). 

## Creating a database

A new database can be created with the `CREATE` command. For example, the following will create a database with the default name of `minhash.db`. The contents of the
new database will be taken from the `roms/` directory specified on the command line

    minhashrom CREATE roms/

The output of this command might look like

    creating db file minhash.db
    chunk size 64
    4928 entries written

The database can be renamed but you can also specify the name as part of the creation process.

    minhashrom CREATE -db roms_v18.db roms/

#### Chunk Size

An important concept in the MinHashROM database is that of `chunk size`. The size of a chunk defines how accurate the fuzzy matching is.By default, the chunk size
is 64 bytes. This value can be changed with the `-c` parameter during creation.

    minhashrom CREATE -c 128 roms/

The chunk size also affects the size of the database. The smaller the chunk value the larger the database. Also note that the chunk value must divide
into 4096 exactly.

The default value of 64 is a good value, there is little reason to change it.
 
## Matching Individual Files

Once a database has been created `MinHashROM` can be used to fuzzy match a ROM file to the contents. For example I have a new ROM file called `new_dump.bin` and I'm
not sure what it is. The following command will search the database:

    minhashrom MATCH new_dump.bin

This will attempt to use the default database name of `minhash.db`. If the datbase has a different name it can be specified with the `-db` parameter.

    minhashrom -db roms_v18.db new_dump.bin

The output of this command in this specific scenario is:

    100.00%   Pitfall! - Pitfall Harry's Jungle Adventure.bin 
    100.00%   Pitfall! - Pitfall Harry's Jungle Adventure.bin 
     96.88%   Pitfall (AKA Pitfall!) (1983) (CCE) (C-813).bin 
     96.88%   Pitfall (AKA Pitfall!) (1983) (Dactari Milmar).bin 
     95.31%   Pitfall (AKA Pitfall!) (1984) (Supergame) (32).bin 
     93.75%   Pitfall (AKA Pitfall!) (Fotomania).bin 
     96.88%   Pitfall (AKA Pitfall!) (Genus).bin 
     85.94%   Pitfall! - Pitfall Harry's Jungle Adventure (Jungle Runner) (03-18-1983) (Activision, David Crane) (AX-018, AX-018-04) (Prototype).bin 
    100.00%   Pitfall! - Pitfall Harry's Jungle Adventure (Jungle Runner) (1982) (Activision, David Crane) (AX-018, AX-018-04) ~.bin 
     93.75%   Pitfall! - Pitfall Harry's Jungle Adventure (Unknown).bin

By default, the command will output all matches with an estimated 80% similarity to the ROM file being searched for (`new_dump.bin` in our example). The sensitivity can be
changed with the `-s` parameter.

    minhashrom -db roms_v18.db -s 65 new_dump.bin

## Searching a Larger File

MinHashRom can be used to `SEARCH` a large file for data that looks similar to the ROMs in the database. This is particularly useful for searching uncompressed tape archives (.tar files and similar).

    minhashrom SEARCH archive.tar

The `SEARCH` mode examines 4096 byte blocks of the larger file in turn, advancing each block one byte at a time. Blocks are searched concurrently, with the default number of concurrent blocks equal to the available number of CPUs. This can be changed with the `-n` parameter.

    minhashrom SEARCH -n 16 archive.tar

Long running searches can interrupted with `CTRL-C` or equivalent signal. When this signal is received, existing blocks will end and a message reporting a `resume byte` will be shown.

    User Interrupt: resume at byte 1983875

The value can be used to restart at the point where the previous session ended.

    minhashrom SEARCH -r 1983875 archive.tar

The sensitivity of the search can be changed with the `-s` parameter. By default sensitivity is set to 80%. In the example below, the sensitivity is reduced to 10%.

    minhashrom SEARCH -s 10 archive.tar

#### Potential Search Matches

Potential matches with ROMs in the database will be shown along with the byte offset of the match. The nature of the search means that you are likely to see several overlapping matches close together. For example, if you see a match at offset 1024, you're likely to see another one nearby:

     93.75%   Yars' Revenge (Canal 3 - Intellivision).bin 
     95.31%   Yars' Revenge (Time Freeze) (1982) (Atari, Howard Scott Warshaw - Sears) (CX2655 - 49-75167) ~.bin 
     85.94%   Yars' Revenge (Unknown) (PAL).bin 
     89.06%   Yars' Revenge (Unknown).bin 
    [Slice at 896 to 4992]
     95.31%   Yars' Revenge (Canal 3 - Intellivision).bin 
     96.88%   Yars' Revenge (Time Freeze) (1982) (Atari, Howard Scott Warshaw - Sears) (CX2655 - 49-75167) ~.bin 
     87.50%   Yars' Revenge (Unknown) (PAL).bin 
     90.62%   Yars' Revenge (Unknown).bin 
    [Slice at 960 to 5056]
     98.44%   Yars' Revenge (Canal 3 - Intellivision).bin 
    100.00%   Yars' Revenge (Time Freeze) (1982) (Atari, Howard Scott Warshaw - Sears) (CX2655 - 49-75167) ~.bin 
     89.06%   Yars' Revenge (Unknown) (PAL).bin 
     93.75%   Yars' Revenge (Unknown).bin 
    [Slice at 1024 to 5120]
     95.31%   Yars' Revenge (Canal 3 - Intellivision).bin 
     96.88%   Yars' Revenge (Time Freeze) (1982) (Atari, Howard Scott Warshaw - Sears) (CX2655 - 49-75167) ~.bin 
     85.94%   Yars' Revenge (Unknown) (PAL).bin 
     90.62%   Yars' Revenge (Unknown).bin 
    [Slice at 1088 to 5184]

In the extract above, we see four matches, starting at byte offset 896, 960, 1024 and 1088. Not all _slices_ will be valid but it is likely that at least one will be. MinHashROM doesn't currently provide any additional tooling to help identify the correct slice.

(Although in this case this case `slice 1024 to 5120` has a 100% match so that slice is a real ROM).

#### Compressed Data

The `SEARCH` mode does not work with tape archives that have been compressed.

