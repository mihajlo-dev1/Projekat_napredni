package wal

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
)

type RecordType uint8

const (
	RecordPut RecordType = iota
	RecordDelete
)

type Record struct {
	Type  RecordType
	Key   []byte
	Value []byte
}

func (r *Record) Serialize() []byte {
	// Format:
	// | type (1B) | keySize (4B) | valueSize (4B) | key | value | crc (4B) |

	keySize := uint32(len(r.Key))
	valueSize := uint32(len(r.Value))

	size := 1 + 4 + 4 + keySize + valueSize
	buf := make([]byte, size+4)

	offset := 0

	buf[offset] = byte(r.Type)
	offset++

	binary.BigEndian.PutUint32(buf[offset:], keySize)
	offset += 4

	binary.BigEndian.PutUint32(buf[offset:], valueSize)
	offset += 4

	copy(buf[offset:], r.Key)
	offset += int(keySize)

	copy(buf[offset:], r.Value)
	offset += int(valueSize)

	crc := crc32.ChecksumIEEE(buf[:offset])
	binary.BigEndian.PutUint32(buf[offset:], crc)

	return buf
}

func ReadRecord(r io.Reader) (*Record, error) {
	var header [9]byte

	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}

	recType := RecordType(header[0])
	keySize := binary.BigEndian.Uint32(header[1:5])
	valueSize := binary.BigEndian.Uint32(header[5:9])

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

	payload := make([]byte, 0, 9+keySize+valueSize)
	payload = append(payload, header[:]...)
	payload = append(payload, key...)
	payload = append(payload, value...)

	actualCRC := crc32.ChecksumIEEE(payload)

	if actualCRC != expectedCRC {
		return nil, errors.New("wal: crc mismatch")
	}

	return &Record{
		Type:  recType,
		Key:   key,
		Value: value,
	}, nil
}
