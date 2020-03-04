package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/iotaledger/iota.go/trinary"
	"github.com/tecbot/gorocksdb"
)

const bloomFilterBitsPerKey = 10
const blockSizeDeviation = 10
const blockRestartInterval = 16
const blockCacheSize = 1000 * 1024
const cacheNumShardBits = 2

var localSnapshotDBKey = func(num int32) []byte {
	intAsByte := make([]byte, 4)
	for i := 3; i >= 0; i-- {
		intAsByte[i] = (byte)(num & 0xFF)
		num >>= 8
	}
	return intAsByte
}(1)

var spentAddrVal = []byte{}

// merge local snapshot and spent addresses db
var localSnapshotsDBTarget = flag.String("ls-db-dir", "./localsnapshots-db", "the name of the folder where the local snapshots database is written to")
var spentAddrDbDir = flag.String("spent-addresses-db-dir", "./spent-addresses-db", "the name of the folder containing the spent addresses database")
var lsStateFileName = flag.String("ls-state-file", "./mainnet.snapshot.state", "the name of the file containing the local snapshot state data")
var lsMetaFileName = flag.String("ls-meta-file", "./mainnet.snapshot.meta", "the name of the file containing the local snapshot meta data")

// export
const expFileVersion byte = 4

var genLSAddrExpFile = flag.Bool("export-db", false, "if enabled, exports all data from a local-snapshot/spent-addresses database into single binary file")
var expFileName = flag.String("export-db-file", "export.bin", "the name of the binary file containing the exported database data")
var printExpDbFileInfo = flag.Bool("export-db-file-info", false, "if enabled, simply prints the specified export file info to the console")
var expOmitSpentAddrs = flag.Bool("omit-spent-addresses", false, "whether to omit exporting spent addresses")

// export spent address
var genAddrExpFile = flag.Bool("export-spent-addr", false, "if enabled, exports all spent addresses from a local-snapshot/spent-addresses database database into single binary file")
var addrExpFileName = flag.String("export-spent-addr-file", "spent_addresses.bin", "the name of the file containing the exported spent addresses")

// merge spent addresses sources
var mergeSpentAddr = flag.Bool("merge-spent-addresses", false, "if enabled, merges multiple source spent-addresses-db databases into one")
var mergeSpentAddrSrcs = flag.String("merge-spent-addresses-sources", "", "the comma separated list of sources of spent-addresses to merge (can be RocksDB spent-addresses-db folders or/and "+
	"text files i.e previousEpochsSpentAddresses.txt (needs to end in .txt)")
var mergeSpentAddrTarget = flag.String("merge-spent-addresses-target", "./merged-spent-addresses-db", "the name of the folder containing the merged spent-addresses-dbs")

// meta
var printLSFilesInfo = flag.Bool("ls-info", false, "if enabled, simply parses the specified local snapshot files and prints their info to the console")

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	flag.Parse()

	fmt.Printf(">> IRI Localsnapshot & SpentAddresses Merger & Exporter v%d <<\n", expFileVersion)

	if *mergeSpentAddr {
		fmt.Println("[merge spent-addresses sources mode]")
		mergeSpentAddressesSources()
		return
	}

	if *printExpDbFileInfo {
		fmt.Println("[print export file info mode]")
		printExportFileInfo()
		return
	}

	if *genLSAddrExpFile {
		fmt.Println("[generate local-snapshot+spent-addresses export file from database mode]")
		generateExportFile()
		return
	}

	if *genAddrExpFile {
		fmt.Println("[generate spent-addresses export file from database mode]")
		generateSpentAddressesExportFile()
		return
	}

	// delete folder
	os.Remove(*localSnapshotsDBTarget)

	if *printLSFilesInfo {
		fmt.Println("[print local snapshot files info mode]")
		printLocalSnapshotFilesInfo(readLocalSnapshotFromFiles())
		return
	}

	fmt.Println("[merge local snapshot files and spent-addresses-db mode]")
	spentAddrChan := make(chan []byte)
	fmt.Println("reading and writing spent addresses database")
	go readSpentAddressesDB(spentAddrChan, *spentAddrDbDir)
	generateLocalSnapshotsDB(spentAddrChan)
}

func mergeSpentAddressesSources() {
	s := time.Now()
	sources := strings.Split(*mergeSpentAddrSrcs, ",")
	if len(sources) < 2 {
		panic("you must define at least 2 spent-addresses sources")
	}

	cfOpt := gorocksdb.NewDefaultOptions()
	cfOpts := []*gorocksdb.Options{cfOpt, cfOpt}

	db, cfs, err := gorocksdb.OpenDbColumnFamilies(defaultOpts(), *mergeSpentAddrTarget, []string{"default", "spent-addresses"}, cfOpts)
	must(err)
	defer db.Close()

	wo := gorocksdb.NewDefaultWriteOptions()
	defer wo.Destroy()

	filter := map[string]struct{}{}

	var count int
	var known int
	for _, source := range sources {
		fmt.Printf("reading in %s\n", source)
		var addrsCount int
		var added int
		if path.Ext(source) == ".txt" {
			f, err := os.OpenFile(source, os.O_RDONLY, 066)
			must(err)
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				addrsCount++
				spentAddrTrytes := scanner.Text()
				spentAddrBytes, err := trinary.TrytesToBytes(spentAddrTrytes)
				must(err)
				filterKey := fmt.Sprintf("%x", sha256.Sum256(spentAddrBytes))
				if _, has := filter[filterKey]; has {
					fmt.Printf("new %d, known %d \t\r", added, known)
					known++
					continue
				}
				must(db.PutCF(wo, cfs[1], spentAddrBytes, spentAddrVal))
				filter[filterKey] = struct{}{}
				added++
				fmt.Printf("new %d, known %d \t\r", added, known)
			}
			must(f.Close())
		} else {
			in := make(chan []byte)
			go readSpentAddressesDB(in, source)

			for spentAddrBytes := range in {
				addrsCount++
				must(err)
				filterKey := fmt.Sprintf("%x", sha256.Sum256(spentAddrBytes))
				if _, has := filter[filterKey]; has {
					known++
					fmt.Printf("new %d, known %d \t\r", added, known)
					continue
				}
				must(db.PutCF(wo, cfs[1], spentAddrBytes, spentAddrVal))
				filter[filterKey] = struct{}{}
				added++
				fmt.Printf("new %d, known %d \t\r", added, known)
			}
		}

		fmt.Printf("new %d, known %d ...done\t\n", added, known)
		known = 0
		count += added
	}

	fmt.Printf("persisted %d spent addresses\n", count)
	fmt.Printf("finished, took %v\n", time.Now().Sub(s))
}

func printExportFileInfo() {
	file, err := os.OpenFile(*expFileName, os.O_RDONLY, 0666)
	must(err)
	defer file.Close()

	var bytesRead int

	var fileVersion byte
	must(binary.Read(file, binary.LittleEndian, &fileVersion))

	if fileVersion != expFileVersion {
		panic(fmt.Sprintf("file version %d is not supported, only version %d", fileVersion, expFileVersion))
	}

	ls := &localsnapshot{
		solidEntryPoints: make(map[string]int32),
		seenMilestones:   make(map[string]int32),
		ledgerState:      make(map[string]uint64),
	}

	// read in milestone hash
	hashBuf := make([]byte, 49)
	_, err = file.Read(hashBuf)
	must(err)
	bytesRead += 49

	lsMsHash, err := trinary.BytesToTrytes(hashBuf)
	must(err)
	ls.msHash = lsMsHash[:81]
	var solidEntryPointsCount, seenMilestonesCount, ledgerEntriesCount, spentAddrsCount int32
	must(binary.Read(file, binary.LittleEndian, &ls.msIndex))
	must(binary.Read(file, binary.LittleEndian, &ls.msTimestamp))
	must(binary.Read(file, binary.LittleEndian, &solidEntryPointsCount))
	must(binary.Read(file, binary.LittleEndian, &seenMilestonesCount))
	must(binary.Read(file, binary.LittleEndian, &ledgerEntriesCount))
	must(binary.Read(file, binary.LittleEndian, &spentAddrsCount))

	// counters
	bytesRead += 28

	// hash based data
	bytesRead += int(solidEntryPointsCount) * (49 + 4)
	bytesRead += int(seenMilestonesCount) * (49 + 4)
	bytesRead += int(ledgerEntriesCount) * (49 + 8)
	bytesRead += int(spentAddrsCount) * (49)

	for i := 0; i < int(solidEntryPointsCount); i++ {
		var val int32
		must(binary.Read(file, binary.LittleEndian, hashBuf))
		must(binary.Read(file, binary.LittleEndian, &val))
		hash, err := trinary.BytesToTrytes(hashBuf)
		must(err)
		ls.solidEntryPoints[hash[:81]] = val
	}

	for i := 0; i < int(seenMilestonesCount); i++ {
		var val int32
		must(binary.Read(file, binary.LittleEndian, hashBuf))
		must(binary.Read(file, binary.LittleEndian, &val))
		hash, err := trinary.BytesToTrytes(hashBuf)
		must(err)
		ls.seenMilestones[hash[:81]] = val
	}

	for i := 0; i < int(ledgerEntriesCount); i++ {
		var val uint64
		must(binary.Read(file, binary.LittleEndian, hashBuf))
		must(binary.Read(file, binary.LittleEndian, &val))
		hash, err := trinary.BytesToTrytes(hashBuf)
		must(err)
		ls.ledgerState[hash[:81]] = val
	}

	for i := 0; i < int(spentAddrsCount); i++ {
		must(binary.Read(file, binary.LittleEndian, hashBuf))
	}

	fmt.Println("file version:", fileVersion)
	fmt.Println("read following local snapshot from the exported database file:")
	printLocalSnapshotFilesInfo(ls)
	fmt.Printf("contains %d spent addresses\n", spentAddrsCount)

	fmt.Printf("read a total of %d KBs\n", bytesRead/1024)
	hashInFile := make([]byte, 32)
	must(binary.Read(file, binary.LittleEndian, hashInFile))

	// read in sha256 hash
	_, err = file.Seek(0, 0)
	must(err)

	// re read file to compute sha256 hash
	fBytes, err := ioutil.ReadAll(file)
	if err != nil {
		panic(err)
	}

	// trim file hash
	fBytes = fBytes[:len(fBytes)-32]
	computedHash := sha256.Sum256(fBytes)
	if !bytes.Equal(hashInFile, computedHash[:]) {
		panic(fmt.Sprintf("computed and sha256 hash do not match: %x (file) vs. %x (computed)", hashInFile, computedHash))
	}
	fmt.Printf("data integrity check successful (sha256): %x\n", computedHash)
}

func generateSpentAddressesExportFile() {
	s := time.Now()

	cfOpt := gorocksdb.NewDefaultOptions()
	cfOpts := []*gorocksdb.Options{cfOpt, cfOpt, cfOpt}

	db, cfs, err := gorocksdb.OpenDbColumnFamilies(defaultOpts(), *localSnapshotsDBTarget, []string{"default", "spent-addresses", "localsnapshots"}, cfOpts)
	must(err)
	defer db.Close()
	ro := gorocksdb.NewDefaultReadOptions()

	fmt.Println("reading in spent addresses...")
	spentAddrs := make([][]byte, 0)
	saIt := db.NewIteratorCF(ro, cfs[1])
	saIt.SeekToFirst()
	for saIt = saIt; saIt.Valid(); saIt.Next() {
		keyCopy := make([]byte, len(saIt.Key().Data()))
		copy(keyCopy, saIt.Key().Data())
		spentAddrs = append(spentAddrs, keyCopy)
		saIt.Key().Free()
		saIt.Value().Free()
	}
	fmt.Printf("read %d spent addresses\n", len(spentAddrs))

	fmt.Println("writing spent addresses...")
	exportFile, err := os.OpenFile(*addrExpFileName, os.O_WRONLY|os.O_CREATE, 0660)
	must(err)

	must(binary.Write(exportFile, binary.LittleEndian, int32(len(spentAddrs))))
	for _, v := range spentAddrs {
		_, err := exportFile.Write(v)
		must(err)
	}

	must(exportFile.Close())

	fmt.Printf("finished, took %v\n", time.Now().Sub(s))
}

func generateExportFile() {
	s := time.Now()

	cfOpt := gorocksdb.NewDefaultOptions()
	cfOpts := []*gorocksdb.Options{cfOpt, cfOpt, cfOpt}

	db, cfs, err := gorocksdb.OpenDbColumnFamilies(defaultOpts(), *localSnapshotsDBTarget, []string{"default", "spent-addresses", "localsnapshots"}, cfOpts)
	must(err)
	defer db.Close()

	// read persisted local snapshot
	ro := gorocksdb.NewDefaultReadOptions()
	lsIt := db.NewIteratorCF(ro, cfs[2])
	lsIt.SeekToFirst()

	if !lsIt.Valid() {
		fmt.Printf("no local snapshot in %s persisted\n", *localSnapshotsDBTarget)
		return
	}

	fmt.Printf("persisted local snapshot is %d KBs in size\n", len(lsIt.Value().Data())/1024)
	ls := readLocalSnapshotFromBytes(lsIt.Value().Data())
	defer lsIt.Key().Free()
	defer lsIt.Value().Free()

	fmt.Println("read following local snapshot from the database:")
	printLocalSnapshotFilesInfo(ls)

	spentAddrs := make([][]byte, 0)
	if *expOmitSpentAddrs {
		fmt.Println("omitting spent addresses in export file")
	} else {
		fmt.Println("reading in spent addresses...")
		saIt := db.NewIteratorCF(ro, cfs[1])
		saIt.SeekToFirst()
		for saIt = saIt; saIt.Valid(); saIt.Next() {
			keyCopy := make([]byte, len(saIt.Key().Data()))
			copy(keyCopy, saIt.Key().Data())
			spentAddrs = append(spentAddrs, keyCopy)
			saIt.Key().Free()
			saIt.Value().Free()
		}
		fmt.Printf("read %d spent addresses\n", len(spentAddrs))
	}

	fmt.Println("writing in-memory binary buffer")
	var buf bytes.Buffer
	msHashBytes, err := trinary.TrytesToBytes(ls.msHash)
	must(err)
	must(binary.Write(&buf, binary.LittleEndian, expFileVersion))
	must(binary.Write(&buf, binary.LittleEndian, msHashBytes))
	must(binary.Write(&buf, binary.LittleEndian, ls.msIndex))
	must(binary.Write(&buf, binary.LittleEndian, ls.msTimestamp))
	must(binary.Write(&buf, binary.LittleEndian, int32(len(ls.solidEntryPoints))))
	must(binary.Write(&buf, binary.LittleEndian, int32(len(ls.seenMilestones))))
	must(binary.Write(&buf, binary.LittleEndian, int32(len(ls.ledgerState))))
	must(binary.Write(&buf, binary.LittleEndian, int32(len(spentAddrs))))

	for k, v := range ls.solidEntryPoints {
		raw, err := trinary.TrytesToBytes(k)
		must(err)
		must(binary.Write(&buf, binary.LittleEndian, raw))
		must(binary.Write(&buf, binary.LittleEndian, v))
	}
	for k, v := range ls.seenMilestones {
		raw, err := trinary.TrytesToBytes(k)
		must(err)
		must(binary.Write(&buf, binary.LittleEndian, raw))
		must(binary.Write(&buf, binary.LittleEndian, v))
	}
	for k, v := range ls.ledgerState {
		raw, err := trinary.TrytesToBytes(k)
		must(err)
		must(binary.Write(&buf, binary.LittleEndian, raw))
		must(binary.Write(&buf, binary.LittleEndian, v))
	}
	for _, v := range spentAddrs {
		must(binary.Write(&buf, binary.LittleEndian, v))
	}

	fmt.Printf("wrote in-memory binary buffer (%d KBs)\n", buf.Len()/1024)
	fmt.Printf("writing binary stream to file %s\n", *expFileName)

	os.Remove(*expFileName)
	exportFile, err := os.OpenFile(*expFileName, os.O_WRONLY|os.O_CREATE, 0660)
	must(err)

	sha256Hash := sha256.Sum256(buf.Bytes())
	must(binary.Write(&buf, binary.LittleEndian, sha256Hash))

	_, err = io.Copy(exportFile, bytes.NewBuffer(buf.Bytes()))
	must(err)

	// clean up
	must(exportFile.Close())

	fmt.Printf("sha256: %x\n", sha256Hash)
	fmt.Printf("finished, took %v\n", time.Now().Sub(s))
}

func defaultOpts() *gorocksdb.Options {
	// db opts
	opts := gorocksdb.NewDefaultOptions()
	opts.SetCreateIfMissing(true)
	opts.SetCreateIfMissingColumnFamilies(true)
	opts.SetMaxOpenFiles(10000)
	opts.SetMaxBackgroundCompactions(1)
	opts.SetMaxLogFileSize(1024 * 1024)
	opts.SetMaxManifestFileSize(1024 * 1024)

	// block based table opts
	bbto := gorocksdb.NewDefaultBlockBasedTableOptions()
	bloomFilter := gorocksdb.NewBloomFilter(bloomFilterBitsPerKey)
	bbto.SetFilterPolicy(bloomFilter)
	bbto.SetBlockSizeDeviation(10)
	bbto.SetBlockRestartInterval(blockRestartInterval)
	bbto.SetBlockSizeDeviation(blockSizeDeviation)
	bbto.SetBlockCache(gorocksdb.NewLRUCache(blockCacheSize))
	opts.SetBlockBasedTableFactory(bbto)

	opts.SetTableCacheNumshardbits(cacheNumShardBits)
	return opts
}

type localsnapshot struct {
	msHash           string
	msIndex          int32
	msTimestamp      int64
	solidEntryPoints map[string]int32
	seenMilestones   map[string]int32
	ledgerState      map[string]uint64
}

func (ls *localsnapshot) SizeInBytes() int {
	return 49 + 20 + (len(ls.solidEntryPoints) * (49 + 4)) + (len(ls.seenMilestones) * (49 + 4)) + (len(ls.ledgerState) * (49 + 8))
}

func (ls *localsnapshot) Bytes() []byte {
	buf := bytes.NewBuffer(make([]byte, 0, ls.SizeInBytes()))
	msHashBytes, err := trinary.TrytesToBytes(ls.msHash)
	must(err)
	must(binary.Write(buf, binary.BigEndian, msHashBytes))
	must(binary.Write(buf, binary.BigEndian, ls.msIndex))
	must(binary.Write(buf, binary.BigEndian, ls.msTimestamp))
	must(binary.Write(buf, binary.BigEndian, int32(len(ls.solidEntryPoints))))
	must(binary.Write(buf, binary.BigEndian, int32(len(ls.seenMilestones))))

	for hash, val := range ls.solidEntryPoints {
		addrBytes, err := trinary.TrytesToBytes(hash)
		must(err)
		must(binary.Write(buf, binary.BigEndian, addrBytes))
		must(binary.Write(buf, binary.BigEndian, val))
	}

	for hash, val := range ls.seenMilestones {
		addrBytes, err := trinary.TrytesToBytes(hash)
		must(err)
		must(binary.Write(buf, binary.BigEndian, addrBytes))
		must(binary.Write(buf, binary.BigEndian, val))
	}

	for hash, val := range ls.ledgerState {
		addrBytes, err := trinary.TrytesToBytes(hash)
		must(err)
		must(binary.Write(buf, binary.BigEndian, addrBytes))
		must(binary.Write(buf, binary.BigEndian, val))
	}
	return buf.Bytes()
}

func printLocalSnapshotFilesInfo(ls *localsnapshot) {
	fmt.Printf("ms index/hash/timestamp: %d/%s/%d\nsolid entry points: %d\nseen milestones: %d\nledger entries: %d\n",
		ls.msIndex, ls.msHash, ls.msTimestamp, len(ls.solidEntryPoints), len(ls.seenMilestones), len(ls.ledgerState))
	var total int64
	for _, val := range ls.ledgerState {
		total += int64(val)
	}
	fmt.Printf("max supply correct: %v\n", total == 2779530283277761)
	fmt.Printf("size: %d KBs\n", ls.SizeInBytes()/1024)
}

func readLocalSnapshotFromBytes(rawLS []byte) *localsnapshot {
	ls := &localsnapshot{
		solidEntryPoints: make(map[string]int32),
		seenMilestones:   make(map[string]int32),
		ledgerState:      make(map[string]uint64),
	}

	hashBuf := make([]byte, 49)
	var solidEntryPointsCount, seenMilestonesCount int32
	buf := bytes.NewBuffer(rawLS)

	// read milestone hash
	must(binary.Read(buf, binary.BigEndian, hashBuf))
	hash, err := trinary.BytesToTrytes(hashBuf)
	must(err)
	ls.msHash = hash[:81]

	// nums
	must(binary.Read(buf, binary.BigEndian, &ls.msIndex))
	must(binary.Read(buf, binary.BigEndian, &ls.msTimestamp))
	must(binary.Read(buf, binary.BigEndian, &solidEntryPointsCount))
	must(binary.Read(buf, binary.BigEndian, &seenMilestonesCount))

	for i := 0; i < int(solidEntryPointsCount); i++ {
		var val int32
		must(binary.Read(buf, binary.BigEndian, hashBuf))
		must(binary.Read(buf, binary.BigEndian, &val))
		hash, err := trinary.BytesToTrytes(hashBuf)
		must(err)
		ls.solidEntryPoints[hash[:81]] = val
	}
	for i := 0; i < int(seenMilestonesCount); i++ {
		var val int32
		must(binary.Read(buf, binary.BigEndian, hashBuf))
		must(binary.Read(buf, binary.BigEndian, &val))
		hash, err := trinary.BytesToTrytes(hashBuf)
		must(err)
		ls.seenMilestones[hash[:81]] = val
	}

	// remaining bytes represent the ledger
	remaining := buf.Len() / (49 + 8)
	for i := 0; i < remaining; i++ {
		var val uint64
		must(binary.Read(buf, binary.BigEndian, hashBuf))
		must(binary.Read(buf, binary.BigEndian, &val))
		hash, err := trinary.BytesToTrytes(hashBuf)
		must(err)
		ls.ledgerState[hash[:81]] = val
	}

	return ls
}

func readLocalSnapshotFromFiles() *localsnapshot {
	ls := &localsnapshot{
		solidEntryPoints: make(map[string]int32),
		seenMilestones:   make(map[string]int32),
		ledgerState:      make(map[string]uint64),
	}
	metaFile, err := os.Open(*lsMetaFileName)
	must(err)
	defer metaFile.Close()

	metaScanner := bufio.NewScanner(metaFile)
	metaScanner.Scan()
	ls.msHash = metaScanner.Text()
	metaScanner.Scan()
	msIndexStr := metaScanner.Text()
	metaScanner.Scan()
	msTimestampStr := metaScanner.Text()
	metaScanner.Scan()
	solidEntryPointsCountStr := metaScanner.Text()
	metaScanner.Scan()
	// skip seen milestones counter

	msIndex, err := strconv.Atoi(msIndexStr)
	must(err)
	ls.msIndex = int32(msIndex)

	ls.msTimestamp, err = strconv.ParseInt(msTimestampStr, 10, 64)
	must(err)

	solidEntryPointsCount, err := strconv.Atoi(solidEntryPointsCountStr)
	must(err)

	for metaScanner.Scan() {
		line := metaScanner.Text()
		split := strings.Split(line, ";")
		hash := split[0]
		valStr := split[1]
		msIndexInt, err := strconv.Atoi(valStr)
		must(err)
		msIndex := int32(msIndexInt)
		if solidEntryPointsCount != 0 {
			ls.solidEntryPoints[hash] = msIndex
			solidEntryPointsCount--
			continue
		}
		ls.seenMilestones[hash] = msIndex
	}

	stateFile, err := os.Open(*lsStateFileName)
	must(err)
	defer stateFile.Close()

	stateScanner := bufio.NewScanner(stateFile)
	for stateScanner.Scan() {
		line := stateScanner.Text()
		split := strings.Split(line, ";")
		addr := split[0]
		val, err := strconv.ParseUint(split[1], 10, 64)
		must(err)
		ls.ledgerState[addr] = val
	}

	return ls
}

func generateLocalSnapshotsDB(in chan []byte) {
	s := time.Now()

	// column family options
	cfOpt := gorocksdb.NewDefaultOptions()
	cfOpts := []*gorocksdb.Options{cfOpt, cfOpt, cfOpt}

	db, cfs, err := gorocksdb.OpenDbColumnFamilies(defaultOpts(), *localSnapshotsDBTarget, []string{"default", "spent-addresses", "localsnapshots"}, cfOpts)
	must(err)
	defer db.Close()

	wo := gorocksdb.NewDefaultWriteOptions()
	defer wo.Destroy()

	var count int
	for spentAddrBytes := range in {
		must(db.PutCF(wo, cfs[1], spentAddrBytes, spentAddrVal))
		count++
		fmt.Printf("%d\t\r", count)
	}
	fmt.Printf("persisted %d spent addresses\n", count)
	fmt.Println("writing local snapshot data...")
	ls := readLocalSnapshotFromFiles()
	printLocalSnapshotFilesInfo(ls)

	// persist local snapshot
	must(db.PutCF(wo, cfs[2], localSnapshotDBKey, ls.Bytes()))

	fmt.Printf("finished, took %v\n", time.Now().Sub(s))
}

func readSpentAddressesDB(out chan []byte, dbDir string) {
	// column family options
	cfOpt := gorocksdb.NewDefaultOptions()
	cfOpts := []*gorocksdb.Options{cfOpt, cfOpt}

	db, cfs, err := gorocksdb.OpenDbColumnFamilies(defaultOpts(), dbDir, []string{"default", "spent-addresses"}, cfOpts)
	must(err)

	ro := gorocksdb.NewDefaultReadOptions()

	it := db.NewIteratorCF(ro, cfs[1])
	it.SeekToFirst()
	for it = it; it.Valid(); it.Next() {
		keyCopy := make([]byte, len(it.Key().Data()))
		copy(keyCopy, it.Key().Data())
		out <- keyCopy
		it.Key().Free()
		it.Value().Free()
	}

	close(out)
}
