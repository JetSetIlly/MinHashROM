# MinHashROM

Fuzzy matching of `Atari 2600` ROM files with a database of precomputed hashes. This is useful for finding the identity of unknown ROM files, particularly those
that may differ by a few bytes. It can also be useful for finding similar ROMs. For example, prototype versions of a game or versions that include custom graphics.

For the fuzzy matching to work a database needs to be created from a collection of existing ROM files. A good ROM collection to start with is the 
[Atari 2600 VCS ROM Collection](http://www.atarimania.com/rom_collection_archive_atari_2600_roms.html). 

### Creating a database

A new database can be created with the `CREATE` command. For example, the following will create a database with the default name of `minhash.db`. The contents of the
new database will be taken from the `roms/` directory specified on the command line

    MinHashROM CREATE roms/

The output of this command might look like

    creating db file minhash.db
    chunk size 64
    4928 entries written

The database can be renamed but you can also specify the name as part of the creation process.

    MinHashROM CREATE -db roms_v18.db roms/

### Searching a database

Once a database has been created `MinHashROM` can be used to fuzzy match a ROM file to the contents. For example I have a new ROM file called `new_dump.bin` and I'm
not sure what it is. The following command will search the database:

    MinHashROM new_dump.bin

This will attempt to use the default database name of `minhash.db`. If the datbase has a different name it can be specified with the `-db` parameter.

    MinHashROM -db roms_v18.db new_dump.bin

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

    MinHashROM -db roms_v18.db -s 65 new_dump.bin

### Chunk size

An important concept in the MinHashROM database is that of `chunk size`. The size of a chunk defines how accurate the fuzzy matching is.By default, the chunk size
is 64 bytes. This value can be changed with the `-c` parameter during creation

    MinHashROM CREATE -c 128 roms/

The chunk size also affects the size of the database. The smaller the chunk value the larger the database. Also note that the chunk value must divide
into 4096 exactly.

### Limitations

This early version of this tool has some limitations. Firstly, it only works with the first 4096 bytes of a ROM file. ROM files of 2048 bytes are handled
by doubling the size to 4096. Future versions will deal with larger ROM files more smartly and use the additional information for better matching.

The second limitation is missing meta-data. More specifically, there is no meta-data for a ROM file besides the filename. This will be expanded on in
future versions.

