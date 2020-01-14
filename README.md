# IRI Localsnapshot & SpentAddresses Merger

|This tool offers following functionality:|
|:----|
| [Combine a `spent-address-db` and local snapshot meta/state files into one `localsnapshots-db` database](#generating-a-localsnapshots-db-from-local-snapshot-files-and-a-spent-addresses-db)|
| [Merge multiple `spent-address-db`s and `previousEpochsSpentAddresses.txt`s into one database](#merging-multiple-spent-addresses-sources)|
| [Generate an export file `export.gz.bin` containing the local snapshot, ledger state and spent-addresses data from a `localsnapshots-db`](#generating-an-export-file-from-a-localsnapshots-db) |
| [Print out infos about a local snapshot given the meta and state files](#print-local-snapshot-infos)|
| [Print out infos about an export file](#print-export-file-infos)|

## Install

Prerequisites: This tool was only tested under Ubuntu 19.04 (shoul also work under 18.04). You also need to have at least Go 1.12.x installed.

1. Git clone the [RocksDB repository](https://github.com/facebook/rocksdb)
2. Go into the newly cloned directory and checkout branch `5.17.fb`
3. Install needed dependencies: `sudo apt-get install libgflags-dev libsnappy-dev zlib1g-dev libbz2-dev libzstd-dev liblz4-dev`
4. Compile the RocksDB shared libs and install them at the appropriate location via: `make shared_lib && make install-shared`
5. `export LD_LIBRARY_PATH=/usr/local/lib`
6. Git clone **this** repository into another directory
7. Get the RocksDB Go wrapper via (replace `/path/to/rocksdb/include` and `/path/to/rocksdb` with the correct paths; 
under Ubuntu 19.04, this might be: `/usr/local/include/rocksdb` and `/usr/local/lib`): 
    ```
    CGO_CFLAGS="-I/path/to/rocksdb/include" \
    CGO_LDFLAGS="-L/path/to/rocksdb -lrocksdb -lstdc++ -lm -lz -lbz2 -lsnappy -llz4 -lzstd" \
      go get github.com/tecbot/gorocksdb
    ```
9. Compile the program using `go build`; if there's no output it means the program has been successfully compiled

## Usage

### Generating a localsnapshots-db from local snapshot files and a spent-addresses-db

Given the above flags, the program per default expects local snapshot files prefixed with "mainnet." and a `spent-addresses-db` in the same folder.
Running the program **without** any argument yields per default a `localsnapshots-db` folder containing RocksDB database data:
```
$ ./iri-ls-sa-merger
reading and writing spent addresses database
persisted 13298777 spent addresses
writing local snapshot data...
ms index/hash/timestamp: 1163676/OX9DIVLRPFNSICOGRTKETSSPXZTTABPZMGS9WXGCLEJOURTFBXPJYMBDGDOZIOCFKHBMJGAHSGLMZ9999/1567592924
solid entry points: 49
seen milestones: 101
ledger entries: 383761
max supply correct: true
bytes size correct: true (21882347 = 21882347)
```

#### Print local snapshot infos
Using `./iri-ls-sa-merger -ls-info` yields information about the local snapshot files:
```
$ ./iri-ls-sa-merger -ls-info
ms index/hash/timestamp: 1163676/OX9DIVLRPFNSICOGRTKETSSPXZTTABPZMGS9WXGCLEJOURTFBXPJYMBDGDOZIOCFKHBMJGAHSGLMZ9999/1567592924
solid entry points: 49
seen milestones: 101
ledger entries: 383761
max supply correct: true
bytes size correct: true (21882347 = 21882347)
```

### Merging multiple spent-addresses sources

Using:
```
$ ./iri-ls-sa-merger -merge-spent-addresses \
-merge-spent-addresses-sources="./spent-addresses-db-1,./spent-addresses-db-2,./previousEpochsSpentAddresses1.txt,./previousEpochsSpentAddresses2.txt,./previousEpochsSpentAddresses3.txt" \
```
Outputs:
```
[merge spent-addresses sources mode]
reading in ./spent-addresses-db-1
new 13298777, known 0 ...done	
reading in ./spent-addresses-db-2
new 17435, known 13298777 ...done	
reading in ./previousEpochsSpentAddresses1.txt
new 0, known 726900 ...done	
reading in ./previousEpochsSpentAddresses2.txt
new 0, known 726871 ...done	
reading in ./previousEpochsSpentAddresses3.txt
new 0, known 996513 ...done	
persisted 13316212 spent addresses
finished, took 5m36.854551752s
```
yields per default a `merged-spent-addresses-db` containing the spent addresses of all specified sources.

### Generating an export file from a localsnapshots-db

Using `./iri-ls-sa-merger -export-db` yields a gzip compressed binary `export.gz.bin` file containing the local snapshot,
ledger state and spent-addresses out of a `localsnapshots-db`. This can be useful for other applications which are reliant on having
the given data in a simple format. You must use different version of the program to read/write different export file versions.

**Note that file format v1 and v2 use big endianness, while v3 uses little endianness.**

<details>
  <summary>File format v1</summary>
  
  File format (decompressed):
  ```
  milestoneHash -> 49 bytes
  milestoneIndex -> int32
  snapshotTimestamp -> int64
  amountOfSolidEntryPoints -> int32
  amountOfSeenMilestones -> int32
  amountOfBalances -> int32
  amountOfSpentAddresses -> int32
  amountOfSolidEntryPoints * solidEntryPointHash:index -> 49 bytes + int32
  amountOfSeenMilestones * seenMilestoneHash:index -> 49 bytes + int32
  amountOfBalances * balance:value -> 49 bytes + int64
  amountOfSpentAddresses * spentAddress -> 49 bytes
  ```    
  
  ``` 
  $ ./iri-ls-sa-merger -export-db
  [export database mode]
  persisted local snapshot is 10 MBs in size
  read following local snapshot from the database:
  ms index/hash/timestamp: 1163676/OX9DIVLRPFNSICOGRTKETSSPXZTTABPZMGS9WXGCLEJOURTFBXPJYMBDGDOZIOCFKHBMJGAHSGLMZ9999/1567592924
  solid entry points: 49
  seen milestones: 101
  ledger entries: 383761
  max supply correct: true
  bytes size correct: true (10 = 10 (MBs))
  reading in spent addresses...
  read 10823853 spent addresses
  writing in-memory binary buffer
  wrote in-memory binary buffer (266 MBs)
  writing gzipped stream to file export.gz.bin
  finished, took 18.609205668s
  ```
  
</details>

<details>
  <summary>File format v2</summary>
  
  File format (decompressed):
  ```
  versionByte -> 1 byte
  milestoneHash -> 49 bytes
  milestoneIndex -> int32
  snapshotTimestamp -> int64
  amountOfSolidEntryPoints -> int32
  amountOfSeenMilestones -> int32
  amountOfBalances -> int32
  amountOfSpentAddresses -> int32
  amountOfSolidEntryPoints * solidEntryPointHash:index -> 49 bytes + int32
  amountOfSeenMilestones * seenMilestoneHash:index -> 49 bytes + int32
  amountOfBalances * balance:value -> 49 bytes + int64
  cuckooFilterSize -> 4 bytes
  cukooFilterData -> N bytes annotated by cuckooFilterSize 
  ```
  
  ```
  $ ./iri-ls-sa-merger -export-db
  [generate export file from database mode]
  persisted local snapshot is 22679 KBs in size
  read following local snapshot from the database:
  ms index/hash/timestamp: 1290028/MQRQLZZYMQNXEDRCULPBHYRJKVHLUV9PXBFFVIPHNPJYGDYBMXHVOEJPYZDVRTBQUUBTYBXDRUAY99999/1577351965
  solid entry points: 58
  seen milestones: 101
  ledger entries: 407290
  max supply correct: true
  size: 22679 (KBs)
  reading in spent addresses...
  read 12927520 spent addresses
  writing in-memory binary buffer
  populating cuckoo filter: 12927520/12927520 (failed to insert: 0)
  spent addresses cuckoo filter size: 65536 KBs
  wrote in-memory binary buffer (88215 KBs)
  writing gzipped stream to file export.gz.bin
  finished, took 2m26.3401733s
  ```
  
</details>

<details>
  <summary>File format v3</summary>
  
  **Note that v3 uses little endianness.**
  
  File format (decompressed):
  ```
  versionByte -> 1 byte
  milestoneHash -> 49 bytes
  milestoneIndex -> int32
  snapshotTimestamp -> int64
  amountOfSolidEntryPoints -> int32
  amountOfSeenMilestones -> int32
  amountOfBalances -> int32
  amountOfSpentAddresses -> int32
  amountOfSolidEntryPoints * solidEntryPointHash:index -> 49 bytes + int32
  amountOfSeenMilestones * seenMilestoneHash:index -> 49 bytes + int32
  amountOfBalances * balance:value -> 49 bytes + int64
  cuckooFilterSize -> 4 bytes
  cukooFilterData -> N bytes annotated by cuckooFilterSize
  sha256 hash of the data above -> 32 bytes
  ```
  
  ```
  $ ./iri-ls-sa-merger -export-db
  [generate export file from database mode]
  persisted local snapshot is 22795 KBs in size
  read following local snapshot from the database:
  ms index/hash/timestamp: 1299871/IONSCLJELWN9FYBOLXC9NJPACEZUTTQEIZPMKRKDFDYVKQHDOCVA9FT9PJSGWSQCJTRSSZAUFGX9Z9999/1578207069
  solid entry points: 114
  seen milestones: 101
  ledger entries: 409313
  max supply correct: true
  size: 22795 KBs
  reading in spent addresses...
  read 12995577 spent addresses
  writing in-memory binary buffer
  populating cuckoo filter: 12995577/12995577 (failed to insert: 0)
  spent addresses cuckoo filter size: 65536 KBs
  wrote in-memory binary buffer (88331 KBs)
  writing gzipped stream to file export.gz.bin
  sha256: a3290e48b6432257e8fb67c1f2540c97b1c54afacc1983ed684d6b4fb011416f
  finished, took 2m7.9454812s
  ```
  
</details>

Note that there are **no** delimiters between the values (there's no `:`), so use the above byte size notation to simply parse
the values accordingly. You can use the verification method's source code to understand on how to write a function
reading in such file.

#### Print export file infos
Using `./iri-ls-sa-merger -export-db-file-info` yields information about the export file:

<details>
  <summary>File format v1</summary>
  
  ```
  $ ./iri-ls-sa-merger -export-db-file-info
  [verify exported database file mode]
  read following local snapshot from the exported database file:
  ms index/hash/timestamp: 1567592924/OX9DIVLRPFNSICOGRTKETSSPXZTTABPZMGS9WXGCLEJOURTFBXPJYMBDGDOZIOCFKHBMJGAHSGLMZ9999/4997950363140096
  solid entry points: 49
  seen milestones: 101
  ledger entries: 383761
  max supply correct: true
  bytes size correct: true (10 = 10 (MBs))
  contains 10823853 spent addresses
  ```
</details>

<details>
  <summary>File format v2</summary>
  
  ```
  $./iri-ls-sa-merger -export-db-file-info
  [print export file info mode]
  file version: 2
  read following local snapshot from the exported database file:
  ms index/hash/timestamp: 1290028/MQRQLZZYMQNXEDRCULPBHYRJKVHLUV9PXBFFVIPHNPJYGDYBMXHVOEJPYZDVRTBQUUBTYBXDRUAY99999/1577351965
  solid entry points: 58
  seen milestones: 101
  ledger entries: 407290
  max supply correct: true
  size: 22679 (KBs)
  spent addresses cuckoo filter size: 65536 KBs
  contains 12927520 spent addresses in the cuckoo filter
  ```
</details>

<details>
  <summary>File format v3</summary>
  
  ```
  ./iri-ls-sa-merger -export-db-file-info
  [print export file info mode]
  file version: 3
  read following local snapshot from the exported database file:
  ms index/hash/timestamp: 1299871/IONSCLJELWN9FYBOLXC9NJPACEZUTTQEIZPMKRKDFDYVKQHDOCVA9FT9PJSGWSQCJTRSSZAUFGX9Z9999/1578207069
  solid entry points: 114
  seen milestones: 101
  ledger entries: 409313
  max supply correct: true
  size: 22795 KBs
  spent addresses cuckoo filter size: 65536 KBs
  contains 12995577 spent addresses in the cuckoo filter
  read a total of 88331 KBs
  data integrity check successful (sha256): a3290e48b6432257e8fb67c1f2540c97b1c54afacc1983ed684d6b4fb011416f
  ```
</details>
