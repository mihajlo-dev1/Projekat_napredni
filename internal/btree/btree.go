package btree

import "kv-engine/internal"

type Node struct {
	keys     []string
	values   [][]byte
	deleted  []bool
	children []*Node
	leaf     bool
}

type BTree struct {
	head    *Node
	order   int
	maxKeys int
	size    int
}

// New pravi prazno B-stablo. order odredjuje maksimalan broj dece po cvoru.
func New(order int) *BTree {
	if order < 3 {
		order = 3
	}

	return &BTree{
		head: &Node{
			leaf: true,
		},
		order:   order,
		maxKeys: order - 1,
		size:    0,
	}
}

// Put prvo pokusa update, a ako kljuc ne postoji radi standardni B-tree insert.
func (t *BTree) Put(key string, value []byte) {
	if t.update(key, value) {
		return
	}

	if len(t.head.keys) == t.maxKeys {
		// Ako je root pun, pravi se novi root i stari se deli.
		oldHead := t.head
		t.head = &Node{
			children: []*Node{oldHead},
			leaf:     false,
		}
		t.splitChild(t.head, 0)
	}

	t.insertNonFull(t.head, key, value)
	t.size++
}

// update trazi postojeci kljuc i menja ga bez menjanja strukture stabla.
func (t *BTree) update(key string, value []byte) bool {
	current := t.head

	for current != nil {
		index := findKeyIndex(current.keys, key)
		if index < len(current.keys) && current.keys[index] == key {
			// Update ozivljava kljuc ako je bio tombstone.
			current.values[index] = append([]byte(nil), value...)
			current.deleted[index] = false
			return true
		}
		if current.leaf {
			return false
		}
		current = current.children[index]
	}

	return false
}

// insertNonFull ubacuje kljuc u cvor za koji znamo da nije pun.
func (t *BTree) insertNonFull(node *Node, key string, value []byte) {
	index := findKeyIndex(node.keys, key)

	if node.leaf {
		// U listu pravimo prazno mesto pa pomeramo vece kljuceve udesno.
		node.keys = append(node.keys, "")
		node.values = append(node.values, nil)
		node.deleted = append(node.deleted, false)

		copy(node.keys[index+1:], node.keys[index:])
		copy(node.values[index+1:], node.values[index:])
		copy(node.deleted[index+1:], node.deleted[index:])

		node.keys[index] = key
		node.values[index] = append([]byte(nil), value...)
		node.deleted[index] = false
		return
	}

	if len(node.children[index].keys) == t.maxKeys {
		// Pre silaska u puno dete, dete se deli da ubacivanje ima mesta.
		t.splitChild(node, index)
		if key > node.keys[index] {
			index++
		}
	}

	t.insertNonFull(node.children[index], key, value)
}

// splitChild deli puno dete na levo dete, srednji kljuc u parent-u i desno dete.
func (t *BTree) splitChild(parent *Node, index int) {
	child := parent.children[index]
	middle := len(child.keys) / 2

	// Desni cvor dobija sve kljuceve posle sredine.
	right := &Node{
		keys:    append([]string(nil), child.keys[middle+1:]...),
		values:  cloneValues(child.values[middle+1:]),
		deleted: append([]bool(nil), child.deleted[middle+1:]...),
		leaf:    child.leaf,
	}
	if !child.leaf {
		right.children = append([]*Node(nil), child.children[middle+1:]...)
		// Levo dete zadrzava samo svoju polovinu dece.
		child.children = child.children[:middle+1]
	}

	// Srednji kljuc se penje u parent.
	middleKey := child.keys[middle]
	middleValue := append([]byte(nil), child.values[middle]...)
	middleDeleted := child.deleted[middle]

	child.keys = child.keys[:middle]
	child.values = child.values[:middle]
	child.deleted = child.deleted[:middle]

	parent.keys = append(parent.keys, "")
	parent.values = append(parent.values, nil)
	parent.deleted = append(parent.deleted, false)
	parent.children = append(parent.children, nil)

	// Parent pravi mesto za srednji kljuc i novo desno dete.
	copy(parent.keys[index+1:], parent.keys[index:])
	copy(parent.values[index+1:], parent.values[index:])
	copy(parent.deleted[index+1:], parent.deleted[index:])
	copy(parent.children[index+2:], parent.children[index+1:])

	parent.keys[index] = middleKey
	parent.values[index] = middleValue
	parent.deleted[index] = middleDeleted
	parent.children[index+1] = right
}

// findKeyIndex vraca prvu poziciju gde key moze da stoji.
func findKeyIndex(keys []string, key string) int {
	index := 0
	for index < len(keys) && keys[index] < key {
		index++
	}
	return index
}

// cloneValues cuva BTree od toga da pozivalac menja byte slice spolja.
func cloneValues(values [][]byte) [][]byte {
	cloned := make([][]byte, len(values))
	for index, value := range values {
		if value != nil {
			cloned[index] = append([]byte(nil), value...)
		}
	}
	return cloned
}

// Get trazi kljuc kroz B-stablo.
func (t *BTree) Get(key string) ([]byte, bool) {
	current := t.head

	for current != nil {
		index := findKeyIndex(current.keys, key)
		if index < len(current.keys) && current.keys[index] == key {
			if current.deleted[index] {
				// Tombstone znaci da kljuc nije vidljiv.
				return nil, false
			}
			return append([]byte(nil), current.values[index]...), true
		}
		if current.leaf {
			return nil, false
		}
		current = current.children[index]
	}

	return nil, false
}

// Delete oznacava kljuc kao tombstone, bez rebalansiranja stabla.
func (t *BTree) Delete(key string) bool {
	current := t.head

	for current != nil {
		index := findKeyIndex(current.keys, key)
		if index < len(current.keys) && current.keys[index] == key {
			if current.deleted[index] {
				return false
			}
			current.deleted[index] = true
			current.values[index] = nil
			return true
		}
		if current.leaf {
			return false
		}
		current = current.children[index]
	}

	return false
}

// Entries vraca sortirane zapise inorder prolaskom.
func (t *BTree) Entries() []internal.MemtableEntry {
	entries := make([]internal.MemtableEntry, 0, t.size)
	collectEntries(t.head, &entries)
	return entries
}

func collectEntries(node *Node, entries *[]internal.MemtableEntry) {
	if node == nil {
		return
	}

	for index, key := range node.keys {
		if !node.leaf {
			// Prvo levo dete, pa kljuc, da rezultat ostane sortiran.
			collectEntries(node.children[index], entries)
		}

		entry := internal.MemtableEntry{
			Key:     key,
			Deleted: node.deleted[index],
		}
		if node.values[index] != nil {
			entry.Value = append([]byte(nil), node.values[index]...)
		}
		*entries = append(*entries, entry)
	}

	if !node.leaf {
		collectEntries(node.children[len(node.keys)], entries)
	}
}
