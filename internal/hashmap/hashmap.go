package hashmap

type Entry struct {
	key     string
	value   []byte
	deleted bool
}

type HashMap struct {
	buckets [][]Entry
	count   int
}

func NewHashMap() HashMap {
	return HashMap{
		buckets: make([][]Entry, 16),
		count:   0,
	}
}

func hash(key string, bucketNumber int) int {
	h := 5381
	for i := 0; i < len(key); i++ {
		h = (h << 5) + h + int(key[i]) // h << 5 shiftovanje za 5 bita u levo
	}
	return h % bucketNumber
}

func (hm *HashMap) Put(key string, value []byte) {
	idx := hash(key, len(hm.buckets))
	bucket := hm.buckets[idx]

	for i := 0; i < len(bucket); i++ {
		if bucket[i].key == key {
			bucket[i].value = value
			if bucket[i].deleted {
				bucket[i].deleted = false
				hm.count++
			}
			hm.buckets[idx] = bucket
			return
		}
	}

	hm.buckets[idx] = append(bucket, Entry{
		key:     key,
		value:   value,
		deleted: false,
	})
	hm.count++
}

func (hm *HashMap) Get(key string) ([]byte, bool) {
	idx := hash(key, len(hm.buckets))
	bucket := hm.buckets[idx]

	for i := 0; i < len(bucket); i++ {
		if bucket[i].key == key && !bucket[i].deleted {
			return bucket[i].value, true
		}
	}

	return nil, false
}

func (hm *HashMap) Delete(key string) bool {
	idx := hash(key, len(hm.buckets))
	bucket := hm.buckets[idx]

	for i := 0; i < len(bucket); i++ {
		if bucket[i].key == key {
			if !bucket[i].deleted {
				bucket[i].deleted = true
				hm.count--
				hm.buckets[idx] = bucket
				return true
			}
		}
	}

	return false
}
