package orderbook

import (
	"math"
	"sync"
)

// Node represents a node in a red-black tree
type Node struct {
	key    float64
	value  *Level
	color  bool // true for red, false for black
	left   *Node
	right  *Node
	parent *Node
}

// PriceLevels is a red-black tree to efficiently store price levels
type PriceLevels struct {
	root      *Node
	sentinel  *Node // Nil sentinel for red-black tree
	ascending bool  // Sort order: true for ascending (asks), false for descending (bids)
	size      int
	mu        sync.RWMutex
}

// Colors for red-black tree nodes
const (
	RED   = true
	BLACK = false
)

// NewPriceLevels creates a new price levels structure
func NewPriceLevels(ascending bool) *PriceLevels {
	sentinel := &Node{color: BLACK}
	return &PriceLevels{
		root:      sentinel,
		sentinel:  sentinel,
		ascending: ascending,
		size:      0,
	}
}

// Insert inserts a price level into the tree
func (pl *PriceLevels) Insert(key float64, value *Level) {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	// Create new node
	z := &Node{
		key:    key,
		value:  value,
		color:  RED,
		left:   pl.sentinel,
		right:  pl.sentinel,
		parent: pl.sentinel,
	}

	// Insert new node
	var y *Node = pl.sentinel
	var x *Node = pl.root

	// Find position to insert
	for x != pl.sentinel {
		y = x
		if pl.compare(z.key, x.key) < 0 {
			x = x.left
		} else if pl.compare(z.key, x.key) > 0 {
			x = x.right
		} else {
			// Key already exists, update value
			x.value = value
			return
		}
	}

	// Set parent
	z.parent = y
	if y == pl.sentinel {
		pl.root = z // Tree was empty
	} else if pl.compare(z.key, y.key) < 0 {
		y.left = z
	} else {
		y.right = z
	}

	// Fix red-black properties
	pl.insertFixup(z)
	pl.size++
}

// Find finds a price level by key
func (pl *PriceLevels) Find(key float64) *Level {
	pl.mu.RLock()
	defer pl.mu.RUnlock()

	node := pl.findNode(key)
	if node == pl.sentinel {
		return nil
	}
	return node.value
}

// Remove removes a price level from the tree
func (pl *PriceLevels) Remove(key float64) bool {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	// Find node to delete
	z := pl.findNode(key)
	if z == pl.sentinel {
		return false // Key not found
	}

	// Remove the node
	pl.deleteNode(z)
	pl.size--
	return true
}

// IsEmpty returns true if the tree is empty
func (pl *PriceLevels) IsEmpty() bool {
	pl.mu.RLock()
	defer pl.mu.RUnlock()
	return pl.root == pl.sentinel
}

// Size returns the number of nodes in the tree
func (pl *PriceLevels) Size() int {
	pl.mu.RLock()
	defer pl.mu.RUnlock()
	return pl.size
}

// FindMax finds the node with the maximum key
func (pl *PriceLevels) FindMax() *Level {
	pl.mu.RLock()
	defer pl.mu.RUnlock()

	node := pl.root
	if node == pl.sentinel {
		return nil
	}

	for node.right != pl.sentinel {
		node = node.right
	}
	return node.value
}

// FindMin finds the node with the minimum key
func (pl *PriceLevels) FindMin() *Level {
	pl.mu.RLock()
	defer pl.mu.RUnlock()

	node := pl.root
	if node == pl.sentinel {
		return nil
	}

	for node.left != pl.sentinel {
		node = node.left
	}
	return node.value
}

// TopN returns the top N price levels
func (pl *PriceLevels) TopN(n int) []*Level {
	pl.mu.RLock()
	defer pl.mu.RUnlock()

	result := make([]*Level, 0, n)
	if pl.root == pl.sentinel {
		return result
	}

	// If ascending, we start from minimum, otherwise from maximum
	var current *Node
	if pl.ascending {
		current = pl.findMinNode()
		pl.inOrderTraversal(current, &result, n)
	} else {
		current = pl.findMaxNode()
		pl.reverseInOrderTraversal(current, &result, n)
	}

	return result
}

// Traverse traverses the tree in order
func (pl *PriceLevels) Traverse(callback func(*Level) bool) {
	pl.mu.RLock()
	defer pl.mu.RUnlock()

	if pl.root == pl.sentinel {
		return
	}

	// Stack to help with traversal
	stack := make([]*Node, 0, pl.size)
	current := pl.root

	for current != pl.sentinel || len(stack) > 0 {
		// Reach the leftmost node
		for current != pl.sentinel {
			stack = append(stack, current)
			current = current.left
		}

		// Stack contains nodes that need to be processed
		if len(stack) > 0 {
			current = stack[len(stack)-1]
			stack = stack[:len(stack)-1]

			// Process the node
			if !callback(current.value) {
				return // Stop traversal if callback returns false
			}

			// Visit right subtree
			current = current.right
		}
	}
}

// insertFixup maintains red-black tree properties after insertion
func (pl *PriceLevels) insertFixup(z *Node) {
	for z.parent.color == RED {
		if z.parent == z.parent.parent.left {
			y := z.parent.parent.right // Uncle

			if y.color == RED {
				// Case 1: Uncle is red
				z.parent.color = BLACK
				y.color = BLACK
				z.parent.parent.color = RED
				z = z.parent.parent
			} else {
				if z == z.parent.right {
					// Case 2: Uncle is black and z is a right child
					z = z.parent
					pl.leftRotate(z)
				}
				// Case 3: Uncle is black and z is a left child
				z.parent.color = BLACK
				z.parent.parent.color = RED
				pl.rightRotate(z.parent.parent)
			}
		} else {
			// Same as above but with left and right swapped
			y := z.parent.parent.left // Uncle

			if y.color == RED {
				// Case 1: Uncle is red
				z.parent.color = BLACK
				y.color = BLACK
				z.parent.parent.color = RED
				z = z.parent.parent
			} else {
				if z == z.parent.left {
					// Case 2: Uncle is black and z is a left child
					z = z.parent
					pl.rightRotate(z)
				}
				// Case 3: Uncle is black and z is a right child
				z.parent.color = BLACK
				z.parent.parent.color = RED
				pl.leftRotate(z.parent.parent)
			}
		}

		// If root is red, make it black
		if z == pl.root {
			break
		}
	}
	pl.root.color = BLACK
}

// leftRotate performs a left rotation on the red-black tree
func (pl *PriceLevels) leftRotate(x *Node) {
	y := x.right
	x.right = y.left

	if y.left != pl.sentinel {
		y.left.parent = x
	}
	y.parent = x.parent

	if x.parent == pl.sentinel {
		pl.root = y
	} else if x == x.parent.left {
		x.parent.left = y
	} else {
		x.parent.right = y
	}
	y.left = x
	x.parent = y
}

// rightRotate performs a right rotation on the red-black tree
func (pl *PriceLevels) rightRotate(y *Node) {
	x := y.left
	y.left = x.right

	if x.right != pl.sentinel {
		x.right.parent = y
	}
	x.parent = y.parent

	if y.parent == pl.sentinel {
		pl.root = x
	} else if y == y.parent.left {
		y.parent.left = x
	} else {
		y.parent.right = x
	}
	x.right = y
	y.parent = x
}

// deleteNode deletes a node from the tree
func (pl *PriceLevels) deleteNode(z *Node) {
	var x, y *Node
	if z.left == pl.sentinel || z.right == pl.sentinel {
		y = z // y is the node to be removed or moved
	} else {
		// Find successor (min element in right subtree)
		y = z.right
		for y.left != pl.sentinel {
			y = y.left
		}
	}

	// Get child of y
	if y.left != pl.sentinel {
		x = y.left
	} else {
		x = y.right
	}

	// Link x to y's parent
	x.parent = y.parent
	if y.parent == pl.sentinel {
		pl.root = x
	} else if y == y.parent.left {
		y.parent.left = x
	} else {
		y.parent.right = x
	}

	// If y is not z, copy y's key and value to z
	if y != z {
		z.key = y.key
		z.value = y.value
	}

	// Fix red-black properties if needed
	if y.color == BLACK {
		pl.deleteFixup(x)
	}
}

// deleteFixup maintains red-black tree properties after deletion
func (pl *PriceLevels) deleteFixup(x *Node) {
	for x != pl.root && x.color == BLACK {
		if x == x.parent.left {
			w := x.parent.right
			if w.color == RED {
				w.color = BLACK
				x.parent.color = RED
				pl.leftRotate(x.parent)
				w = x.parent.right
			}
			if w.left.color == BLACK && w.right.color == BLACK {
				w.color = RED
				x = x.parent
			} else {
				if w.right.color == BLACK {
					w.left.color = BLACK
					w.color = RED
					pl.rightRotate(w)
					w = x.parent.right
				}
				w.color = x.parent.color
				x.parent.color = BLACK
				w.right.color = BLACK
				pl.leftRotate(x.parent)
				x = pl.root
			}
		} else {
			// Same as above but with left and right swapped
			w := x.parent.left
			if w.color == RED {
				w.color = BLACK
				x.parent.color = RED
				pl.rightRotate(x.parent)
				w = x.parent.left
			}
			if w.right.color == BLACK && w.left.color == BLACK {
				w.color = RED
				x = x.parent
			} else {
				if w.left.color == BLACK {
					w.right.color = BLACK
					w.color = RED
					pl.leftRotate(w)
					w = x.parent.left
				}
				w.color = x.parent.color
				x.parent.color = BLACK
				w.left.color = BLACK
				pl.rightRotate(x.parent)
				x = pl.root
			}
		}
	}
	x.color = BLACK
}

// findNode finds a node by key
func (pl *PriceLevels) findNode(key float64) *Node {
	current := pl.root
	for current != pl.sentinel {
		comp := pl.compare(key, current.key)
		if comp < 0 {
			current = current.left
		} else if comp > 0 {
			current = current.right
		} else {
			return current
		}
	}
	return pl.sentinel
}

// findMinNode finds the node with the minimum key
func (pl *PriceLevels) findMinNode() *Node {
	if pl.root == pl.sentinel {
		return pl.sentinel
	}

	current := pl.root
	for current.left != pl.sentinel {
		current = current.left
	}
	return current
}

// findMaxNode finds the node with the maximum key
func (pl *PriceLevels) findMaxNode() *Node {
	if pl.root == pl.sentinel {
		return pl.sentinel
	}

	current := pl.root
	for current.right != pl.sentinel {
		current = current.right
	}
	return current
}

// compare compares two keys based on the sort order
func (pl *PriceLevels) compare(a, b float64) int {
	// Handle floating point comparison with epsilon for near-equality
	epsilon := 1e-9
	diff := a - b

	if math.Abs(diff) < epsilon {
		return 0
	}

	if pl.ascending {
		if diff < 0 {
			return -1
		}
		return 1
	} else {
		if diff < 0 {
			return 1
		}
		return -1
	}
}

// inOrderTraversal performs in-order traversal (ascending)
func (pl *PriceLevels) inOrderTraversal(node *Node, result *[]*Level, limit int) {
	if node == pl.sentinel || len(*result) >= limit {
		return
	}

	pl.inOrderTraversal(node.left, result, limit)

	if len(*result) < limit {
		*result = append(*result, node.value)
		pl.inOrderTraversal(node.right, result, limit)
	}
}

// reverseInOrderTraversal performs reverse in-order traversal (descending)
func (pl *PriceLevels) reverseInOrderTraversal(node *Node, result *[]*Level, limit int) {
	if node == pl.sentinel || len(*result) >= limit {
		return
	}

	pl.reverseInOrderTraversal(node.right, result, limit)

	if len(*result) < limit {
		*result = append(*result, node.value)
		pl.reverseInOrderTraversal(node.left, result, limit)
	}
}

// CalculateImbalance calculates the order book imbalance
func CalculateImbalance(bidVolume, askVolume float64) float64 {
	if bidVolume+askVolume == 0 {
		return 0
	}
	return (bidVolume - askVolume) / (bidVolume + askVolume)
}
