package zipstream

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"

	"zip-forger/internal/source"
)

type RangeOpenFunc func(ctx context.Context, filePath string, start, end int64) (io.ReadCloser, error)
type CRCFunc func(ctx context.Context, entry source.Entry) (uint32, error)
type RecordCRCFunc func(entry source.Entry, crc uint32)

type VirtualArchive struct {
	entries               []virtualEntry
	centralDirectorySize  int64
	centralDirectoryStart int64
	size                  int64
	zip64                 bool
}

type virtualEntry struct {
	source.Entry
	localHeaderOffset int64
	dataOffset        int64
	descriptorOffset  int64
	localHeader       []byte
	descriptorLen     int64
	zip64Offset       bool
}

const (
	localFileHeaderSignature      = 0x04034b50
	dataDescriptorSignature       = 0x08074b50
	centralDirectorySignature     = 0x02014b50
	endOfCentralDirectorySig      = 0x06054b50
	zip64EndOfCentralDirectorySig = 0x06064b50
	zip64LocatorSignature         = 0x07064b50
	dataDescriptorFlag            = 0x0008
	storeMethod                   = 0
	version20                     = 20
	version45                     = 45
	fixedDOSTime                  = 0
	fixedDOSDate                  = 33 // 1980-01-01
	maxUint16Value                = int(^uint16(0))
	maxUint32Value                = int64(^uint32(0))
)

func NewVirtualArchive(entries []source.Entry) (*VirtualArchive, error) {
	out := &VirtualArchive{
		entries: make([]virtualEntry, 0, len(entries)),
	}

	cursor := int64(0)
	for _, entry := range entries {
		if entry.Size < 0 {
			return nil, fmt.Errorf("zipstream: negative entry size for %s", entry.Path)
		}
		if entry.Size > maxUint32Value {
			return nil, fmt.Errorf("zipstream: entries larger than 4 GiB are not yet supported: %s", entry.Path)
		}

		localHeader := buildLocalFileHeader(entry.Path)
		item := virtualEntry{
			Entry:             entry,
			localHeaderOffset: cursor,
			dataOffset:        cursor + int64(len(localHeader)),
			localHeader:       localHeader,
			descriptorLen:     16,
		}
		item.descriptorOffset = item.dataOffset + entry.Size
		item.zip64Offset = item.localHeaderOffset > maxUint32Value

		out.entries = append(out.entries, item)
		cursor = item.descriptorOffset + item.descriptorLen
	}

	out.centralDirectoryStart = cursor
	for _, entry := range out.entries {
		out.centralDirectorySize += int64(centralDirectoryRecordLen(entry))
	}

	out.zip64 = len(out.entries) > maxUint16Value ||
		out.centralDirectoryStart > maxUint32Value ||
		out.centralDirectorySize > maxUint32Value
	if !out.zip64 {
		for _, entry := range out.entries {
			if entry.zip64Offset {
				out.zip64 = true
				break
			}
		}
	}

	out.size = out.centralDirectoryStart + out.centralDirectorySize + int64(endRecordsLen(out.zip64))
	return out, nil
}

func (a *VirtualArchive) Size() int64 {
	return a.size
}

func (a *VirtualArchive) StreamRange(
	ctx context.Context,
	w io.Writer,
	start, end int64,
	open OpenFunc,
	openRange RangeOpenFunc,
	resolveCRC CRCFunc,
	recordCRC RecordCRCFunc,
) error {
	if start < 0 {
		start = 0
	}
	if end < 0 || end > a.size {
		end = a.size
	}
	if start > end {
		start = end
	}

	computed := make(map[string]uint32, len(a.entries))
	getCRC := func(ctx context.Context, entry source.Entry) (uint32, error) {
		if value, ok := computed[entry.Path]; ok {
			return value, nil
		}
		return resolveCRC(ctx, entry)
	}

	for _, entry := range a.entries {
		if err := ctx.Err(); err != nil {
			return err
		}

		var (
			dataReader io.ReadCloser
			copySource io.Reader
			copyLen    int64
			hasher     hash32
		)
		if overlapStart, overlapEnd, ok := overlap(entry.dataOffset, entry.descriptorOffset, start, end); ok {
			rangeStart := overlapStart - entry.dataOffset
			rangeLen := overlapEnd - overlapStart

			reader, err := openArchiveFileRange(ctx, entry.Entry, rangeStart, rangeLen, open, openRange)
			if err != nil {
				return err
			}

			dataReader = reader
			copySource = reader
			copyLen = rangeLen
			if overlapStart == entry.dataOffset && overlapEnd == entry.descriptorOffset {
				hasher = crc32.NewIEEE()
				copySource = io.TeeReader(reader, hasher)
			}
		}

		if err := writeLiteralRange(w, entry.localHeaderOffset, entry.localHeader, start, end); err != nil {
			if dataReader != nil {
				dataReader.Close()
			}
			return err
		}

		if dataReader != nil {
			_, copyErr := io.CopyN(w, copySource, copyLen)
			closeErr := dataReader.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}

			if hasher != nil {
				crc := hasher.Sum32()
				computed[entry.Path] = crc
				if recordCRC != nil {
					recordCRC(entry.Entry, crc)
				}
			}
		}

		if overlapStart, overlapEnd, ok := overlap(entry.descriptorOffset, entry.descriptorOffset+entry.descriptorLen, start, end); ok {
			crc, err := getCRC(ctx, entry.Entry)
			if err != nil {
				return err
			}
			descriptor := buildDataDescriptor(entry.Size, crc)
			if err := writeLiteralSubrange(w, descriptor, overlapStart-entry.descriptorOffset, overlapEnd-overlapStart); err != nil {
				return err
			}
		}
	}

	if overlapStart, overlapEnd, ok := overlap(a.centralDirectoryStart, a.size, start, end); ok {
		tail, err := a.buildTail(ctx, getCRC)
		if err != nil {
			return err
		}
		if err := writeLiteralSubrange(w, tail, overlapStart-a.centralDirectoryStart, overlapEnd-overlapStart); err != nil {
			return err
		}
	}

	return nil
}

func (a *VirtualArchive) buildTail(ctx context.Context, resolveCRC CRCFunc) ([]byte, error) {
	var tail bytes.Buffer

	for _, entry := range a.entries {
		crc, err := resolveCRC(ctx, entry.Entry)
		if err != nil {
			return nil, err
		}
		writeCentralDirectoryRecord(&tail, entry, crc)
	}

	if a.zip64 {
		writeZip64EndRecords(&tail, len(a.entries), a.centralDirectorySize, a.centralDirectoryStart)
	}
	writeEndOfCentralDirectory(&tail, len(a.entries), a.centralDirectorySize, a.centralDirectoryStart, a.zip64)
	return tail.Bytes(), nil
}

func openArchiveFileRange(
	ctx context.Context,
	entry source.Entry,
	start, length int64,
	open OpenFunc,
	openRange RangeOpenFunc,
) (io.ReadCloser, error) {
	if length <= 0 {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}
	end := start + length
	if openRange != nil {
		return openRange(ctx, entry.Path, start, end)
	}

	reader, err := open(ctx, entry.Path)
	if err != nil {
		return nil, err
	}
	if start > 0 {
		if _, err := io.CopyN(io.Discard, reader, start); err != nil {
			reader.Close()
			return nil, err
		}
	}
	return readCloser{
		Reader: io.LimitReader(reader, length),
		Closer: reader,
	}, nil
}

func buildLocalFileHeader(name string) []byte {
	nameBytes := []byte(name)
	header := make([]byte, 30+len(nameBytes))
	binary.LittleEndian.PutUint32(header[0:4], localFileHeaderSignature)
	binary.LittleEndian.PutUint16(header[4:6], version20)
	binary.LittleEndian.PutUint16(header[6:8], dataDescriptorFlag)
	binary.LittleEndian.PutUint16(header[8:10], storeMethod)
	binary.LittleEndian.PutUint16(header[10:12], fixedDOSTime)
	binary.LittleEndian.PutUint16(header[12:14], fixedDOSDate)
	binary.LittleEndian.PutUint16(header[26:28], uint16(len(nameBytes)))
	copy(header[30:], nameBytes)
	return header
}

func buildDataDescriptor(size int64, crc uint32) []byte {
	descriptor := make([]byte, 16)
	binary.LittleEndian.PutUint32(descriptor[0:4], dataDescriptorSignature)
	binary.LittleEndian.PutUint32(descriptor[4:8], crc)
	binary.LittleEndian.PutUint32(descriptor[8:12], uint32(size))
	binary.LittleEndian.PutUint32(descriptor[12:16], uint32(size))
	return descriptor
}

func centralDirectoryRecordLen(entry virtualEntry) int {
	return 46 + len(entry.Path) + centralDirectoryExtraLen(entry)
}

func centralDirectoryExtraLen(entry virtualEntry) int {
	if entry.zip64Offset {
		return 12
	}
	return 0
}

func writeCentralDirectoryRecord(w io.Writer, entry virtualEntry, crc uint32) {
	nameBytes := []byte(entry.Path)
	extraLen := centralDirectoryExtraLen(entry)
	record := make([]byte, 46+len(nameBytes)+extraLen)
	versionNeeded := uint16(version20)
	if entry.zip64Offset {
		versionNeeded = version45
	}

	binary.LittleEndian.PutUint32(record[0:4], centralDirectorySignature)
	binary.LittleEndian.PutUint16(record[4:6], versionNeeded)
	binary.LittleEndian.PutUint16(record[6:8], versionNeeded)
	binary.LittleEndian.PutUint16(record[8:10], dataDescriptorFlag)
	binary.LittleEndian.PutUint16(record[10:12], storeMethod)
	binary.LittleEndian.PutUint16(record[12:14], fixedDOSTime)
	binary.LittleEndian.PutUint16(record[14:16], fixedDOSDate)
	binary.LittleEndian.PutUint32(record[16:20], crc)
	binary.LittleEndian.PutUint32(record[20:24], uint32(entry.Size))
	binary.LittleEndian.PutUint32(record[24:28], uint32(entry.Size))
	binary.LittleEndian.PutUint16(record[28:30], uint16(len(nameBytes)))
	binary.LittleEndian.PutUint16(record[30:32], uint16(extraLen))
	if entry.zip64Offset {
		binary.LittleEndian.PutUint32(record[42:46], ^uint32(0))
	} else {
		binary.LittleEndian.PutUint32(record[42:46], uint32(entry.localHeaderOffset))
	}
	copy(record[46:], nameBytes)
	if entry.zip64Offset {
		extraOffset := 46 + len(nameBytes)
		binary.LittleEndian.PutUint16(record[extraOffset:extraOffset+2], 0x0001)
		binary.LittleEndian.PutUint16(record[extraOffset+2:extraOffset+4], 8)
		binary.LittleEndian.PutUint64(record[extraOffset+4:extraOffset+12], uint64(entry.localHeaderOffset))
	}
	_, _ = w.Write(record)
}

func writeZip64EndRecords(w io.Writer, entryCount int, cdSize, cdOffset int64) {
	record := make([]byte, 56)
	binary.LittleEndian.PutUint32(record[0:4], zip64EndOfCentralDirectorySig)
	binary.LittleEndian.PutUint64(record[4:12], 44)
	binary.LittleEndian.PutUint16(record[12:14], version45)
	binary.LittleEndian.PutUint16(record[14:16], version45)
	binary.LittleEndian.PutUint64(record[24:32], uint64(entryCount))
	binary.LittleEndian.PutUint64(record[32:40], uint64(entryCount))
	binary.LittleEndian.PutUint64(record[40:48], uint64(cdSize))
	binary.LittleEndian.PutUint64(record[48:56], uint64(cdOffset))
	_, _ = w.Write(record)

	offset := cdOffset + cdSize
	locator := make([]byte, 20)
	binary.LittleEndian.PutUint32(locator[0:4], zip64LocatorSignature)
	binary.LittleEndian.PutUint64(locator[8:16], uint64(offset))
	binary.LittleEndian.PutUint32(locator[16:20], 1)
	_, _ = w.Write(locator)
}

func writeEndOfCentralDirectory(w io.Writer, entryCount int, cdSize, cdOffset int64, zip64 bool) {
	record := make([]byte, 22)
	binary.LittleEndian.PutUint32(record[0:4], endOfCentralDirectorySig)
	if zip64 {
		binary.LittleEndian.PutUint16(record[8:10], ^uint16(0))
		binary.LittleEndian.PutUint16(record[10:12], ^uint16(0))
		binary.LittleEndian.PutUint32(record[12:16], ^uint32(0))
		binary.LittleEndian.PutUint32(record[16:20], ^uint32(0))
	} else {
		binary.LittleEndian.PutUint16(record[8:10], uint16(entryCount))
		binary.LittleEndian.PutUint16(record[10:12], uint16(entryCount))
		binary.LittleEndian.PutUint32(record[12:16], uint32(cdSize))
		binary.LittleEndian.PutUint32(record[16:20], uint32(cdOffset))
	}
	_, _ = w.Write(record)
}

func endRecordsLen(zip64 bool) int {
	if zip64 {
		return 56 + 20 + 22
	}
	return 22
}

func overlap(segStart, segEnd, reqStart, reqEnd int64) (int64, int64, bool) {
	if reqStart >= segEnd || reqEnd <= segStart {
		return 0, 0, false
	}
	start := segStart
	if reqStart > start {
		start = reqStart
	}
	end := segEnd
	if reqEnd < end {
		end = reqEnd
	}
	return start, end, start < end
}

func writeLiteralRange(w io.Writer, offset int64, data []byte, start, end int64) error {
	subStart, length, ok := subrange(offset, int64(len(data)), start, end)
	if !ok {
		return nil
	}
	return writeLiteralSubrange(w, data, subStart, length)
}

func writeLiteralSubrange(w io.Writer, data []byte, subStart, length int64) error {
	if length <= 0 {
		return nil
	}
	_, err := w.Write(data[subStart : subStart+length])
	return err
}

func subrange(offset, dataLen, start, end int64) (int64, int64, bool) {
	segStart, segEnd, ok := overlap(offset, offset+dataLen, start, end)
	if !ok {
		return 0, 0, false
	}
	return segStart - offset, segEnd - segStart, true
}

type hash32 interface {
	io.Writer
	Sum32() uint32
}

type readCloser struct {
	io.Reader
	io.Closer
}
