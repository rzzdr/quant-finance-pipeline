package orderbook

import (
	"fmt"
	"sync"
)

type Color int

const (
	Red Color = iota
	Black
)

type Node struct {
	Price  float64
	Level  *PriceLevel
	Color  Color
	Left   *Node
	Right  *Node
	Parent *Node
}

type RedBlackTree struct {
	root  *Node
	nil   *Node // Sentinel node
	mutex sync.RWMutex
	isBid bool // true for bid tree (max heap), false for ask tree (min heap)
}

func NewRedBlackTree(isBid bool) *RedBlackTree {
	nilNode := &Node{Color: Black}
	return &RedBlackTree{
		root:  nilNode,
		nil:   nilNode,
		isBid: isBid,
	}
}

func (tree *RedBlackTree) Insert(price float64) *PriceLevel {
	tree.mutex.Lock()
	defer tree.mutex.Unlock()

	if existing := tree.searchNode(price); existing != tree.nil {
		return existing.Level
	}

	level := NewPriceLevel(price)
	node := &Node{
		Price:  price,
		Level:  level,
		Color:  Red,
		Left:   tree.nil,
		Right:  tree.nil,
		Parent: tree.nil,
	}

	tree.insertNode(node)
	tree.insertFixup(node)

	return level
}

func (tree *RedBlackTree) Delete(price float64) {
	tree.mutex.Lock()
	defer tree.mutex.Unlock()

	node := tree.searchNode(price)
	if node == tree.nil {
		return
	}

	tree.deleteNode(node)
}

func (tree *RedBlackTree) Search(price float64) *PriceLevel {
	tree.mutex.RLock()
	defer tree.mutex.RUnlock()

	node := tree.searchNode(price)
	if node == tree.nil {
		return nil
	}
	return node.Level
}

func (tree *RedBlackTree) GetBest() *PriceLevel {
	tree.mutex.RLock()
	defer tree.mutex.RUnlock()

	if tree.root == tree.nil {
		return nil
	}

	var node *Node
	if tree.isBid {
		node = tree.maximum(tree.root)
	} else {
		node = tree.minimum(tree.root)
	}

	if node == tree.nil {
		return nil
	}
	return node.Level
}

func (tree *RedBlackTree) GetLevels(maxLevels int) []*PriceLevel {
	tree.mutex.RLock()
	defer tree.mutex.RUnlock()

	var levels []*PriceLevel
	var current *Node

	if tree.root == tree.nil {
		return levels
	}

	if tree.isBid {
		current = tree.maximum(tree.root)
		for current != tree.nil && len(levels) < maxLevels {
			if !current.Level.IsEmpty() {
				levels = append(levels, current.Level)
			}
			current = tree.predecessor(current)
		}
	} else {
		current = tree.minimum(tree.root)
		for current != tree.nil && len(levels) < maxLevels {
			if !current.Level.IsEmpty() {
				levels = append(levels, current.Level)
			}
			current = tree.successor(current)
		}
	}

	return levels
}

func (tree *RedBlackTree) searchNode(price float64) *Node {
	node := tree.root
	for node != tree.nil {
		if price == node.Price {
			return node
		} else if price < node.Price {
			node = node.Left
		} else {
			node = node.Right
		}
	}
	return tree.nil
}

func (tree *RedBlackTree) insertNode(z *Node) {
	y := tree.nil
	x := tree.root

	for x != tree.nil {
		y = x
		if z.Price < x.Price {
			x = x.Left
		} else {
			x = x.Right
		}
	}

	z.Parent = y
	if y == tree.nil {
		tree.root = z
	} else if z.Price < y.Price {
		y.Left = z
	} else {
		y.Right = z
	}
}

func (tree *RedBlackTree) insertFixup(z *Node) {
	for z.Parent.Color == Red {
		if z.Parent == z.Parent.Parent.Left {
			y := z.Parent.Parent.Right
			if y.Color == Red {
				z.Parent.Color = Black
				y.Color = Black
				z.Parent.Parent.Color = Red
				z = z.Parent.Parent
			} else {
				if z == z.Parent.Right {
					z = z.Parent
					tree.leftRotate(z)
				}
				z.Parent.Color = Black
				z.Parent.Parent.Color = Red
				tree.rightRotate(z.Parent.Parent)
			}
		} else {
			y := z.Parent.Parent.Left
			if y.Color == Red {
				z.Parent.Color = Black
				y.Color = Black
				z.Parent.Parent.Color = Red
				z = z.Parent.Parent
			} else {
				if z == z.Parent.Left {
					z = z.Parent
					tree.rightRotate(z)
				}
				z.Parent.Color = Black
				z.Parent.Parent.Color = Red
				tree.leftRotate(z.Parent.Parent)
			}
		}
	}
	tree.root.Color = Black
}

func (tree *RedBlackTree) deleteNode(z *Node) {
	y := z
	yOriginalColor := y.Color
	var x *Node

	if z.Left == tree.nil {
		x = z.Right
		tree.transplant(z, z.Right)
	} else if z.Right == tree.nil {
		x = z.Left
		tree.transplant(z, z.Left)
	} else {
		y = tree.minimum(z.Right)
		yOriginalColor = y.Color
		x = y.Right
		if y.Parent == z {
			x.Parent = y
		} else {
			tree.transplant(y, y.Right)
			y.Right = z.Right
			y.Right.Parent = y
		}
		tree.transplant(z, y)
		y.Left = z.Left
		y.Left.Parent = y
		y.Color = z.Color
	}

	if yOriginalColor == Black {
		tree.deleteFixup(x)
	}
}

func (tree *RedBlackTree) deleteFixup(x *Node) {
	for x != tree.root && x.Color == Black {
		if x == x.Parent.Left {
			w := x.Parent.Right
			if w.Color == Red {
				w.Color = Black
				x.Parent.Color = Red
				tree.leftRotate(x.Parent)
				w = x.Parent.Right
			}
			if w.Left.Color == Black && w.Right.Color == Black {
				w.Color = Red
				x = x.Parent
			} else {
				if w.Right.Color == Black {
					w.Left.Color = Black
					w.Color = Red
					tree.rightRotate(w)
					w = x.Parent.Right
				}
				w.Color = x.Parent.Color
				x.Parent.Color = Black
				w.Right.Color = Black
				tree.leftRotate(x.Parent)
				x = tree.root
			}
		} else {
			w := x.Parent.Left
			if w.Color == Red {
				w.Color = Black
				x.Parent.Color = Red
				tree.rightRotate(x.Parent)
				w = x.Parent.Left
			}
			if w.Right.Color == Black && w.Left.Color == Black {
				w.Color = Red
				x = x.Parent
			} else {
				if w.Left.Color == Black {
					w.Right.Color = Black
					w.Color = Red
					tree.leftRotate(w)
					w = x.Parent.Left
				}
				w.Color = x.Parent.Color
				x.Parent.Color = Black
				w.Left.Color = Black
				tree.rightRotate(x.Parent)
				x = tree.root
			}
		}
	}
	x.Color = Black
}

func (tree *RedBlackTree) leftRotate(x *Node) {
	y := x.Right
	x.Right = y.Left
	if y.Left != tree.nil {
		y.Left.Parent = x
	}
	y.Parent = x.Parent
	if x.Parent == tree.nil {
		tree.root = y
	} else if x == x.Parent.Left {
		x.Parent.Left = y
	} else {
		x.Parent.Right = y
	}
	y.Left = x
	x.Parent = y
}

func (tree *RedBlackTree) rightRotate(x *Node) {
	y := x.Left
	x.Left = y.Right
	if y.Right != tree.nil {
		y.Right.Parent = x
	}
	y.Parent = x.Parent
	if x.Parent == tree.nil {
		tree.root = y
	} else if x == x.Parent.Right {
		x.Parent.Right = y
	} else {
		x.Parent.Left = y
	}
	y.Right = x
	x.Parent = y
}

func (tree *RedBlackTree) transplant(u, v *Node) {
	if u.Parent == tree.nil {
		tree.root = v
	} else if u == u.Parent.Left {
		u.Parent.Left = v
	} else {
		u.Parent.Right = v
	}
	v.Parent = u.Parent
}

func (tree *RedBlackTree) minimum(node *Node) *Node {
	for node.Left != tree.nil {
		node = node.Left
	}
	return node
}

func (tree *RedBlackTree) maximum(node *Node) *Node {
	for node.Right != tree.nil {
		node = node.Right
	}
	return node
}

func (tree *RedBlackTree) successor(x *Node) *Node {
	if x.Right != tree.nil {
		return tree.minimum(x.Right)
	}
	y := x.Parent
	for y != tree.nil && x == y.Right {
		x = y
		y = y.Parent
	}
	return y
}

func (tree *RedBlackTree) predecessor(x *Node) *Node {
	if x.Left != tree.nil {
		return tree.maximum(x.Left)
	}
	y := x.Parent
	for y != tree.nil && x == y.Left {
		x = y
		y = y.Parent
	}
	return y
}

func (tree *RedBlackTree) Size() int {
	tree.mutex.RLock()
	defer tree.mutex.RUnlock()
	return tree.sizeRecursive(tree.root)
}

func (tree *RedBlackTree) sizeRecursive(node *Node) int {
	if node == tree.nil {
		return 0
	}
	return 1 + tree.sizeRecursive(node.Left) + tree.sizeRecursive(node.Right)
}

func (tree *RedBlackTree) Print() {
	tree.mutex.RLock()
	defer tree.mutex.RUnlock()
	tree.printRecursive(tree.root, "", true)
}

func (tree *RedBlackTree) printRecursive(node *Node, prefix string, isLast bool) {
	if node == tree.nil {
		return
	}

	color := "R"
	if node.Color == Black {
		color = "B"
	}

	fmt.Printf("%s", prefix)
	if isLast {
		fmt.Printf("└── ")
	} else {
		fmt.Printf("├── ")
	}
	fmt.Printf("%.2f(%s)\n", node.Price, color)

	newPrefix := prefix
	if isLast {
		newPrefix += "    "
	} else {
		newPrefix += "│   "
	}

	tree.printRecursive(node.Left, newPrefix, false)
	tree.printRecursive(node.Right, newPrefix, true)
}
