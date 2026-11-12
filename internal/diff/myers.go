package diff

// Port of org.eclipse.jgit.diff.MyersDiff.

type myers struct {
	edits []*Edit
	a, b  seq
	m     *middleEdit
}

func myersDiff(edits []*Edit, a, b seq, region *Edit) []*Edit {
	md := &myers{edits: edits, a: a, b: b}
	md.m = newMiddleEdit(md)
	md.calculateEditsRegion(region)
	return md.edits
}

func (md *myers) calculateEditsRegion(r *Edit) {
	m := md.m
	m.initialize(r.BeginA, r.EndA, r.BeginB, r.EndB)
	if m.beginA >= m.endA && m.beginB >= m.endB {
		return
	}
	md.calculateEdits(m.beginA, m.endA, m.beginB, m.endB)
}

func (md *myers) calculateEdits(beginA, endA, beginB, endB int) {
	m := md.m
	edit := m.calculate(beginA, endA, beginB, endB)
	if beginA < edit.BeginA || beginB < edit.BeginB {
		k := edit.BeginB - edit.BeginA
		x := m.backward.snake(k, edit.BeginA)
		md.calculateEdits(beginA, x, beginB, k+x)
	}
	if edit.typ() != typeEmpty {
		md.edits = append(md.edits, edit)
	}
	if endA > edit.EndA || endB > edit.EndB {
		k := edit.EndB - edit.EndA
		x := m.forward.snake(k, edit.EndA)
		md.calculateEdits(x, endA, k+x, endB)
	}
}

type middleEdit struct {
	md                         *myers
	forward, backward          *editPaths
	beginA, endA, beginB, endB int
	edit                       *Edit
}

func newMiddleEdit(md *myers) *middleEdit {
	m := &middleEdit{md: md}
	m.forward = &editPaths{m: m, fwd: true}
	m.backward = &editPaths{m: m, fwd: false}
	return m
}

func (m *middleEdit) initialize(beginA, endA, beginB, endB int) {
	m.beginA, m.endA, m.beginB, m.endB = beginA, endA, beginB, endB
	k := beginB - beginA
	m.beginA = m.forward.snake(k, beginA)
	m.beginB = k + m.beginA
	k = endB - endA
	m.endA = m.backward.snake(k, endA)
	m.endB = k + m.endA
}

func (m *middleEdit) calculate(beginA, endA, beginB, endB int) *Edit {
	if beginA == endA || beginB == endB {
		return &Edit{beginA, endA, beginB, endB}
	}
	m.beginA, m.endA, m.beginB, m.endB = beginA, endA, beginB, endB
	minK := beginB - endA
	maxK := endB - beginA
	m.forward.initialize(beginB-beginA, beginA, minK, maxK)
	m.backward.initialize(endB-endA, endA, minK, maxK)
	for d := 1; ; d++ {
		if m.forward.calculate(d) || m.backward.calculate(d) {
			return m.edit
		}
	}
}

type editPaths struct {
	m                     *middleEdit
	fwd                   bool
	x                     []int
	snakes                []int64
	beginK, endK, middleK int
	prevBeginK, prevEndK  int
	minK, maxK            int
}

func (p *editPaths) other() *editPaths {
	if p.fwd {
		return p.m.backward
	}
	return p.m.forward
}

func (p *editPaths) getIndex(d, k int) int {
	if (d+k-p.middleK)%2 != 0 {
		panic("unexpected odd result")
	}
	return (d + k - p.middleK) / 2
}

func (p *editPaths) getX(d, k int) int {
	if k < p.beginK || k > p.endK {
		panic("k not in range")
	}
	return p.x[p.getIndex(d, k)]
}

func (p *editPaths) getSnake(d, k int) int64 {
	if k < p.beginK || k > p.endK {
		panic("k not in range")
	}
	return p.snakes[p.getIndex(d, k)]
}

func (p *editPaths) forceKIntoRange(k int) int {
	if k < p.minK {
		return p.minK + ((k ^ p.minK) & 1)
	} else if k > p.maxK {
		return p.maxK - ((k ^ p.maxK) & 1)
	}
	return k
}

func (p *editPaths) initialize(k, x, minK, maxK int) {
	p.minK, p.maxK = minK, maxK
	p.beginK, p.endK, p.middleK = k, k, k
	p.x = append(p.x[:0], x)
	p.snakes = append(p.snakes[:0], newSnake(k, x))
}

func newSnake(k, x int) int64 {
	y := int64(k + x)
	return int64(x)<<32 | y
}
func snake2x(s int64) int { return int(int32(uint64(s) >> 32)) }
func snake2y(s int64) int { return int(int32(s)) }

func (p *editPaths) makeEdit(snake1, snake2 int64) bool {
	x1, x2 := snake2x(snake1), snake2x(snake2)
	y1, y2 := snake2y(snake1), snake2y(snake2)
	if x1 > x2 || y1 > y2 {
		x1 = x2
		y1 = y2
	}
	p.m.edit = &Edit{x1, x2, y1, y2}
	return true
}

func setAt[T any](list []T, i int, v T) []T {
	if i == len(list) {
		return append(list, v)
	}
	if i > len(list) {
		panic("index out of bounds")
	}
	list[i] = v
	return list
}

func (p *editPaths) calculate(d int) bool {
	p.prevBeginK = p.beginK
	p.prevEndK = p.endK
	p.beginK = p.forceKIntoRange(p.middleK - d)
	p.endK = p.forceKIntoRange(p.middleK + d)
	for k := p.endK; k >= p.beginK; k -= 2 {
		left, right := -1, -1
		leftSnake, rightSnake := int64(-1), int64(-1)
		if k > p.prevBeginK {
			i := p.getIndex(d-1, k-1)
			left = p.x[i]
			end := p.snake(k-1, left)
			if left != end {
				leftSnake = newSnake(k-1, end)
			} else {
				leftSnake = p.snakes[i]
			}
			if p.meets(d, k-1, end, leftSnake) {
				return true
			}
			left = p.getLeft(end)
		}
		if k < p.prevEndK {
			i := p.getIndex(d-1, k+1)
			right = p.x[i]
			end := p.snake(k+1, right)
			if right != end {
				rightSnake = newSnake(k+1, end)
			} else {
				rightSnake = p.snakes[i]
			}
			if p.meets(d, k+1, end, rightSnake) {
				return true
			}
			right = p.getRight(end)
		}
		var newX int
		var ns int64
		if k >= p.prevEndK || (k > p.prevBeginK && p.isBetter(left, right)) {
			newX = left
			ns = leftSnake
		} else {
			newX = right
			ns = rightSnake
		}
		if p.meets(d, k, newX, ns) {
			return true
		}
		p.adjustMinMaxK(k, newX)
		i := p.getIndex(d, k)
		p.x = setAt(p.x, i, newX)
		p.snakes = setAt(p.snakes, i, ns)
	}
	return false
}

func (p *editPaths) snake(k, x int) int {
	m := p.m
	a, b := m.md.a, m.md.b
	if p.fwd {
		for ; x < m.endA && k+x < m.endB; x++ {
			if !seqEquals(a, x, b, k+x) {
				break
			}
		}
		return x
	}
	for ; x > m.beginA && k+x > m.beginB; x-- {
		if !seqEquals(a, x-1, b, k+x-1) {
			break
		}
	}
	return x
}

func (p *editPaths) getLeft(x int) int {
	if p.fwd {
		return x
	}
	return x - 1
}

func (p *editPaths) getRight(x int) int {
	if p.fwd {
		return x + 1
	}
	return x
}

func (p *editPaths) isBetter(left, right int) bool {
	if p.fwd {
		return left > right
	}
	return left < right
}

func (p *editPaths) adjustMinMaxK(k, x int) {
	m := p.m
	o := p.other()
	if p.fwd {
		if x >= m.endA || k+x >= m.endB {
			if k > o.middleK {
				p.maxK = k
			} else {
				p.minK = k
			}
		}
		return
	}
	if x <= m.beginA || k+x <= m.beginB {
		if k > o.middleK {
			p.maxK = k
		} else {
			p.minK = k
		}
	}
}

func (p *editPaths) meets(d, k, x int, snake int64) bool {
	o := p.other()
	if k < o.beginK || k > o.endK {
		return false
	}
	if p.fwd {
		if (d-1+k-o.middleK)%2 != 0 {
			return false
		}
		if x < o.getX(d-1, k) {
			return false
		}
		return p.makeEdit(snake, o.getSnake(d-1, k))
	}
	if (d+k-o.middleK)%2 != 0 {
		return false
	}
	if x > o.getX(d, k) {
		return false
	}
	return p.makeEdit(o.getSnake(d, k), snake)
}
