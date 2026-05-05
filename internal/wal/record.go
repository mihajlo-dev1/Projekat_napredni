package wal

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"

	"kv-engine/internal"
)

const (
	RecordFull   internal.RecordType = 10
	RecordFirst  internal.RecordType = 11
	RecordMiddle internal.RecordType = 12
	RecordLast   internal.RecordType = 13
)

type frame struct {
	fragmentType internal.RecordType
	data         []byte
}

type frameReader struct {
	r           io.Reader
	blockOffset int
}

func newFrameReader(r io.Reader) *frameReader {
	return &frameReader{r: r}
}

func SerializeRecord(record *internal.Record) []byte {
	keySize := uint32(len(record.Key))
	valueSize := uint32(len(record.Value))

	size := 1 + 8 + 8 + 4 + 4 + keySize + valueSize
	buf := make([]byte, size+4)

	offset := 0

	buf[offset] = byte(record.Type)
	offset++

	binary.BigEndian.PutUint64(buf[offset:], uint64(record.Timestamp))
	offset += 8

	binary.BigEndian.PutUint64(buf[offset:], uint64(record.TTL))
	offset += 8

	binary.BigEndian.PutUint32(buf[offset:], keySize)
	offset += 4

	binary.BigEndian.PutUint32(buf[offset:], valueSize)
	offset += 4

	copy(buf[offset:], record.Key)
	offset += int(keySize)

	copy(buf[offset:], record.Value)
	offset += int(valueSize)

	crc := crc32.ChecksumIEEE(buf[:offset])
	binary.BigEndian.PutUint32(buf[offset:], crc)

	return buf
}

func DeserializeRecord(data []byte) (*internal.Record, error) {
	if len(data) < 29 {
		return nil, io.ErrUnexpectedEOF
	}

	header := data[:25]
	recordType := internal.RecordType(header[0])
	timestamp := int64(binary.BigEndian.Uint64(header[1:9]))
	ttl := int64(binary.BigEndian.Uint64(header[9:17]))
	keySize := binary.BigEndian.Uint32(header[17:21])
	valueSize := binary.BigEndian.Uint32(header[21:25])

	totalSize := 25 + int(keySize) + int(valueSize) + 4
	if len(data) < totalSize {
		return nil, io.ErrUnexpectedEOF
	}
	if len(data) > totalSize {
		return nil, fmt.Errorf("wal: trailing bytes in record")
	}

	keyStart := 25
	keyEnd := keyStart + int(keySize)
	valueEnd := keyEnd + int(valueSize)

	expectedCRC := binary.BigEndian.Uint32(data[valueEnd:totalSize])
	actualCRC := crc32.ChecksumIEEE(data[:valueEnd])
	if actualCRC != expectedCRC {
		return nil, errors.New("wal: crc mismatch")
	}

	key := append([]byte(nil), data[keyStart:keyEnd]...)
	value := append([]byte(nil), data[keyEnd:valueEnd]...)

	return &internal.Record{
		Key:       key,
		Value:     value,
		Type:      recordType,
		Timestamp: timestamp,
		TTL:       ttl,
	}, nil
}

func ReadRecord(r io.Reader) (*internal.Record, error) {
	var header [25]byte

	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}

	recordType := internal.RecordType(header[0])
	timestamp := int64(binary.BigEndian.Uint64(header[1:9]))
	ttl := int64(binary.BigEndian.Uint64(header[9:17]))
	keySize := binary.BigEndian.Uint32(header[17:21])
	valueSize := binary.BigEndian.Uint32(header[21:25])

	key := make([]byte, keySize)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, err
	}

	value := make([]byte, valueSize)
	if _, err := io.ReadFull(r, value); err != nil {
		return nil, err
	}

	var crcBuf [4]byte
	if _, err := io.ReadFull(r, crcBuf[:]); err != nil {
		return nil, err
	}

	expectedCRC := binary.BigEndian.Uint32(crcBuf[:])

	payload := make([]byte, 0, len(header)+len(key)+len(value))
	payload = append(payload, header[:]...)
	payload = append(payload, key...)
	payload = append(payload, value...)

	actualCRC := crc32.ChecksumIEEE(payload)
	if actualCRC != expectedCRC {
		return nil, errors.New("wal: crc mismatch")
	}

	return &internal.Record{
		Key:       key,
		Value:     value,
		Type:      recordType,
		Timestamp: timestamp,
		TTL:       ttl,
	}, nil
}

func writeFrame(w io.Writer, fragmentType internal.RecordType, data []byte) (int, error) {
	var header [frameHeaderSize]byte
	header[0] = byte(fragmentType)
	binary.BigEndian.PutUint32(header[1:], uint32(len(data)))

	if _, err := w.Write(header[:]); err != nil {
		return 0, err
	}
	if _, err := w.Write(data); err != nil {
		return 0, err
	}

	return frameHeaderSize + len(data), nil
}

func ReadNextRecord(r *frameReader) (*internal.Record, error) {
	var assembled []byte
	inFragmentedRecord := false

	for {
		f, err := readFrame(r)
		if err != nil {
			if err == io.EOF && len(assembled) > 0 {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}

		switch f.fragmentType {
		case RecordFull:
			if inFragmentedRecord {
				return nil, errors.New("wal: full fragment inside fragmented record")
			}
			return DeserializeRecord(f.data)
		case RecordFirst:
			if inFragmentedRecord {
				return nil, errors.New("wal: first fragment inside fragmented record")
			}
			assembled = append(assembled[:0], f.data...)
			inFragmentedRecord = true
		case RecordMiddle:
			if !inFragmentedRecord {
				return nil, errors.New("wal: middle fragment without first fragment")
			}
			assembled = append(assembled, f.data...)
		case RecordLast:
			if !inFragmentedRecord {
				return nil, errors.New("wal: last fragment without first fragment")
			}
			assembled = append(assembled, f.data...)
			return DeserializeRecord(assembled)
		default:
			return nil, fmt.Errorf("wal: unknown fragment type %d", f.fragmentType)
		}
	}
}

func readFrame(r *frameReader) (frame, error) {
	for {
		if remaining := blockSize - r.blockOffset; remaining < frameHeaderSize && remaining != blockSize {
			padding := make([]byte, remaining)
			if _, err := io.ReadFull(r.r, padding); err != nil {
				return frame{}, err
			}
			r.blockOffset = 0
			continue
		}

		var header [frameHeaderSize]byte
		if _, err := io.ReadFull(r.r, header[:]); err != nil {
			if err == io.ErrUnexpectedEOF {
				return frame{}, err
			}
			return frame{}, err
		}
		r.blockOffset += frameHeaderSize

		fragmentType := internal.RecordType(header[0])
		length := int(binary.BigEndian.Uint32(header[1:]))
		if length < 0 || length > blockSize {
			return frame{}, fmt.Errorf("wal: invalid frame length %d", length)
		}
		if length == 0 && fragmentType == 0 {
			r.blockOffset = 0
			continue
		}
		if length > blockSize-r.blockOffset {
			return frame{}, fmt.Errorf("wal: frame crosses block boundary")
		}

		data := make([]byte, length)
		if _, err := io.ReadFull(r.r, data); err != nil {
			return frame{}, err
		}
		r.blockOffset += length
		if r.blockOffset == blockSize {
			r.blockOffset = 0
		}

		return frame{fragmentType: fragmentType, data: data}, nil
	}
}
