package lb

type rbColor bool

const (
	black rbColor = false
	red   rbColor = true
)

type rbNode struct {
	index  int
	left   *rbNode
	right  *rbNode
	parent *rbNode
	color  rbColor
}

type rbTree struct {
	root *rbNode
	sentinel *rbNode
	size int
}

func newRBTree() *rbTree {
	sentinel := &rbNode{index: -1, color: black}
	return &rbTree{
		root:     sentinel,
		sentinel: sentinel,
	}
}

func (t *rbTree) min() *rbNode {
	if t.root == t.sentinel {
		return nil
	}
	n := t.root
	for n.left != t.sentinel {
		n = n.left
	}
	return n
}

func (t *rbTree) max() *rbNode {
	if t.root == t.sentinel {
		return nil
	}
	n := t.root
	for n.right != t.sentinel {
		n = n.right
	}
	return n
}

func (t *rbTree) insert(less func(a, b *rbNode) bool, index int) *rbNode {
	return t.insertNode(&rbNode{index: index, color: red, left: t.sentinel, right: t.sentinel, parent: t.sentinel}, less)
}

func (t *rbTree) insertNode(z *rbNode, less func(a, b *rbNode) bool) *rbNode {

	y := t.sentinel
	x := t.root
	for x != t.sentinel {
		y = x
		if less(z, x) {
			x = x.left
		} else {
			x = x.right
		}
	}
	z.parent = y
	if y == t.sentinel {
		t.root = z
	} else if less(z, y) {
		y.left = z
	} else {
		y.right = z
	}
	t.insertFixup(z)
	t.size++
	return z
}

func (t *rbTree) delete(z *rbNode) {
	if t.sentinel == nil || z == t.sentinel {
		return
	}
	y := z
	yOrigColor := y.color
	var x *rbNode

	switch {
	case z.left == t.sentinel:
		x = z.right
		t.transplant(z, z.right)
	case z.right == t.sentinel:
		x = z.left
		t.transplant(z, z.left)
	default:
		y = z.right
		for y.left != t.sentinel {
			y = y.left
		}
		yOrigColor = y.color
		x = y.right
		if y.parent == z {
			x.parent = y
		} else {
			t.transplant(y, y.right)
			y.right = z.right
			y.right.parent = y
		}
		t.transplant(z, y)
		y.left = z.left
		y.left.parent = y
		y.color = z.color
	}
	if yOrigColor == black {
		t.deleteFixup(x)
	}
	t.size--
}

func (t *rbTree) transplant(u, v *rbNode) {
	if u.parent == t.sentinel {
		t.root = v
	} else if u == u.parent.left {
		u.parent.left = v
	} else {
		u.parent.right = v
	}
	v.parent = u.parent
}

func (t *rbTree) rotateLeft(x *rbNode) {
	y := x.right
	x.right = y.left
	if y.left != t.sentinel {
		y.left.parent = x
	}
	y.parent = x.parent
	if x.parent == t.sentinel {
		t.root = y
	} else if x == x.parent.left {
		x.parent.left = y
	} else {
		x.parent.right = y
	}
	y.left = x
	x.parent = y
}

func (t *rbTree) rotateRight(x *rbNode) {
	y := x.left
	x.left = y.right
	if y.right != t.sentinel {
		y.right.parent = x
	}
	y.parent = x.parent
	if x.parent == t.sentinel {
		t.root = y
	} else if x == x.parent.right {
		x.parent.right = y
	} else {
		x.parent.left = y
	}
	y.right = x
	x.parent = y
}

func (t *rbTree) insertFixup(z *rbNode) {
	for z.parent.color == red {
		if z.parent == z.parent.parent.left {
			uncle := z.parent.parent.right
			if uncle.color == red {
				z.parent.color = black
				uncle.color = black
				z.parent.parent.color = red
				z = z.parent.parent
			} else {
				if z == z.parent.right {
					z = z.parent
					t.rotateLeft(z)
				}
				z.parent.color = black
				z.parent.parent.color = red
				t.rotateRight(z.parent.parent)
			}
		} else {
			uncle := z.parent.parent.left
			if uncle.color == red {
				z.parent.color = black
				uncle.color = black
				z.parent.parent.color = red
				z = z.parent.parent
			} else {
				if z == z.parent.left {
					z = z.parent
					t.rotateRight(z)
				}
				z.parent.color = black
				z.parent.parent.color = red
				t.rotateLeft(z.parent.parent)
			}
		}
	}
	t.root.color = black
}

func (t *rbTree) deleteFixup(x *rbNode) {
	for x != t.root && x.color == black {
		if x == x.parent.left {
			w := x.parent.right
			if w.color == red {
				w.color = black
				x.parent.color = red
				t.rotateLeft(x.parent)
				w = x.parent.right
			}
			if w.left.color == black && w.right.color == black {
				w.color = red
				x = x.parent
			} else {
				if w.right.color == black {
					w.left.color = black
					w.color = red
					t.rotateRight(w)
					w = x.parent.right
				}
				w.color = x.parent.color
				x.parent.color = black
				w.right.color = black
				t.rotateLeft(x.parent)
				x = t.root
			}
		} else {
			w := x.parent.left
			if w.color == red {
				w.color = black
				x.parent.color = red
				t.rotateRight(x.parent)
				w = x.parent.left
			}
			if w.right.color == black && w.left.color == black {
				w.color = red
				x = x.parent
			} else {
				if w.left.color == black {
					w.right.color = black
					w.color = red
					t.rotateLeft(w)
					w = x.parent.left
				}
				w.color = x.parent.color
				x.parent.color = black
				w.left.color = black
				t.rotateRight(x.parent)
				x = t.root
			}
		}
	}
	x.color = black
}

// rebuildRBTree rebuilds a red-black tree and position map for load balancers
// t: pointer to rbTree (will be replaced with new instance)
// posMap: pointer to position map slice (will be reallocated)
// n: number of nodes
// less: comparison function for tree ordering
func rebuildRBTree(t **rbTree, posMap *[]*rbNode, n int, less func(a, b *rbNode) bool) {
	*t = newRBTree()
	*posMap = make([]*rbNode, n)
	for i := 0; i < n; i++ {
		node := (*t).insert(less, i)
		(*posMap)[i] = node
	}
}
