package wal

import (
	"encoding/binary"
	"errors"
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
