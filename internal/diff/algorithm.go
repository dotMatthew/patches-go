package diff

import "math/bits"

// Edit is a JGit Edit: lines [BeginA, EndA) of A replaced by [BeginB, EndB) of B.
type Edit struct{ BeginA, EndA, BeginB, EndB int }

type editType int

const (
	typeInsert editType = iota
	typeDelete
	typeReplace
	typeEmpty
)

func (e *Edit) typ() editType {
	if e.BeginA < e.EndA {
		if e.BeginB < e.EndB {
			return typeReplace
		}
		return typeDelete
	}
	if e.BeginB < e.EndB {
		return typeInsert
	}
	return typeEmpty
}

func (e *Edit) lenA() int     { return e.EndA - e.BeginA }
func (e *Edit) lenB() int     { return e.EndB - e.BeginB }
func (e *Edit) isEmpty() bool { return e.BeginA == e.EndA && e.BeginB == e.EndB }
func (e *Edit) shift(amount int) {
	e.BeginA += amount
	e.EndA += amount
	e.BeginB += amount
	e.EndB += amount
}
func (e *Edit) before(cut *Edit) *Edit {
	return &Edit{e.BeginA, cut.BeginA, e.BeginB, cut.BeginB}
}
func (e *Edit) after(cut *Edit) *Edit {
	return &Edit{cut.EndA, e.EndA, cut.EndB, e.EndB}
}

// Algorithm selects the diff algorithm (diff.algorithm config).
type Algorithm int

const (
	Histogram Algorithm = iota
	Myers
)

// seq is a HashedSequence over a Subsequence of a RawText.
type seq struct {
	t   *RawText
	off int
	n   int
}

func (s seq) size() int        { return s.n }
func (s seq) hash(i int) int32 { return s.t.hash(s.off + i) }
func seqEquals(a seq, ai int, b seq, bi int) bool {
	return hashedEquals(a.t, a.off+ai, b.t, b.off+bi)
}

// Diff computes the edit list between a and b (DiffAlgorithm.diff).
func Diff(alg Algorithm, a, b *RawText) []*Edit {
	region := &Edit{0, a.Size(), 0, b.Size()}
	reduceCommonStartEnd(a, b, region)
	switch region.typ() {
	case typeInsert, typeDelete:
		return []*Edit{region}
	case typeReplace:
		if region.lenA() == 1 && region.lenB() == 1 {
			return []*Edit{region}
		}
		as := seq{a, region.BeginA, region.lenA()}
		bs := seq{b, region.BeginB, region.lenB()}
		var edits []*Edit
		r := &Edit{0, as.size(), 0, bs.size()}
		if alg == Myers {
			edits = myersDiff(edits, as, bs, r)
		} else {
			st := &histState{a: as, b: bs}
			st.diffRegion(r)
			edits = st.edits
		}
		for _, e := range edits {
			e.BeginA += region.BeginA
			e.EndA += region.BeginA
			e.BeginB += region.BeginB
			e.EndB += region.BeginB
		}
		return normalize(edits, a, b)
	default:
		return nil
	}
}

func normalize(e []*Edit, a, b *RawText) []*Edit {
	var prev *Edit
	for i := len(e) - 1; i >= 0; i-- {
		cur := e[i]
		curType := cur.typ()
		maxA, maxB := a.Size(), b.Size()
		if prev != nil {
			maxA, maxB = prev.BeginA, prev.BeginB
		}
		switch curType {
		case typeInsert:
			for cur.EndA < maxA && cur.EndB < maxB && equals(b, cur.BeginB, b, cur.EndB) {
				cur.shift(1)
			}
		case typeDelete:
			for cur.EndA < maxA && cur.EndB < maxB && equals(a, cur.BeginA, a, cur.EndA) {
				cur.shift(1)
			}
		}
		prev = cur
	}
	return e
}

// reduceCommonStartEnd is RawTextComparator.reduceCommonStartEnd followed by
// SequenceComparator.reduceCommonStartEnd.
func reduceCommonStartEnd(a, b *RawText, e *Edit) {
	if e.BeginA == e.EndA || e.BeginB == e.EndB {
		return
	}
	aRaw, bRaw := a.Content, b.Content
	aPtr := a.lines[e.BeginA+1]
	bPtr := a.lines[e.BeginB+1] // sic: JGit uses a.lines here as well
	aEnd := a.lines[e.EndA+1]
	bEnd := b.lines[e.EndB+1]
	for aPtr < aEnd && bPtr < bEnd && aRaw[aPtr] == bRaw[bPtr] {
		aPtr++
		bPtr++
	}
	for aPtr < aEnd && bPtr < bEnd && aRaw[aEnd-1] == bRaw[bEnd-1] {
		aEnd--
		bEnd--
	}
	e.BeginA = findForwardLine(a.lines, e.BeginA, aPtr)
	e.BeginB = findForwardLine(b.lines, e.BeginB, bPtr)
	e.EndA = findReverseLine(a.lines, e.EndA, aEnd)
	partialA := aEnd < a.lines[e.EndA+1]
	if partialA {
		bEnd += a.lines[e.EndA+1] - aEnd
	}
	e.EndB = findReverseLine(b.lines, e.EndB, bEnd)
	if !partialA && bEnd < b.lines[e.EndB+1] {
		e.EndA++
	}
	// SequenceComparator.reduceCommonStartEnd
	for e.BeginA < e.EndA && e.BeginB < e.EndB && equals(a, e.BeginA, b, e.BeginB) {
		e.BeginA++
		e.BeginB++
	}
	for e.BeginA < e.EndA && e.BeginB < e.EndB && equals(a, e.EndA-1, b, e.EndB-1) {
		e.EndA--
		e.EndB--
	}
}

func findForwardLine(lines []int, idx, ptr int) int {
	end := len(lines) - 2
	for idx < end && lines[idx+2] < ptr {
		idx++
	}
	return idx
}

func findReverseLine(lines []int, idx, ptr int) int {
	for 0 < idx && ptr <= lines[idx] {
		idx--
	}
	return idx
}

// ---------------------------------------------------------------- Histogram

const maxChainLength = 64

type histState struct {
	a, b  seq
	queue []*Edit
	edits []*Edit
}

func (s *histState) diffRegion(r *Edit) {
	s.diffReplace(r)
	for len(s.queue) > 0 {
		r := s.queue[len(s.queue)-1]
		s.queue = s.queue[:len(s.queue)-1]
		s.diff(r)
	}
}

func (s *histState) diffReplace(r *Edit) {
	lcs := newHistIndex(s.a, s.b, r).findLongestCommonSequence()
	if lcs != nil {
		if lcs.isEmpty() {
			s.edits = append(s.edits, r)
		} else {
			s.queue = append(s.queue, r.after(lcs))
			s.queue = append(s.queue, r.before(lcs))
		}
	} else {
		s.edits = myersDiff(s.edits, s.a, s.b, r)
	}
}

func (s *histState) diff(r *Edit) {
	switch r.typ() {
	case typeInsert, typeDelete:
		s.edits = append(s.edits, r)
	case typeReplace:
		if r.lenA() == 1 && r.lenB() == 1 {
			s.edits = append(s.edits, r)
		} else {
			s.diffReplace(r)
		}
	default:
		panic("illegal state")
	}
}

const (
	recNextShift = 28 + 8
	recPtrShift  = 8
	recPtrMask   = (1 << 28) - 1
	recCntMask   = (1 << 8) - 1
	maxCnt       = (1 << 8) - 1
)

type histIndex struct {
	a, b      seq
	region    *Edit
	table     []int32
	keyShift  uint
	recs      []int64
	recCnt    int
	next      []int32
	recIdx    []int32
	ptrShift  int
	lcs       *Edit
	cnt       int
	hasCommon bool
}

func newHistIndex(a, b seq, r *Edit) *histIndex {
	sz := r.lenA()
	tb := tableBits(sz)
	recsLen := sz >> 3
	if recsLen < 4 {
		recsLen = 4
	}
	return &histIndex{
		a: a, b: b, region: r,
		table:    make([]int32, 1<<tb),
		keyShift: uint(32 - tb),
		ptrShift: r.BeginA,
		recs:     make([]int64, recsLen),
		next:     make([]int32, sz),
		recIdx:   make([]int32, sz),
	}
}

func tableBits(sz int) int {
	b := 31 - bits.LeadingZeros32(uint32(sz))
	if b == 0 {
		b = 1
	}
	if 1<<b < sz {
		b++
	}
	return b
}

func recCreate(next, ptr, cnt int) int64 {
	return int64(next)<<recNextShift | int64(ptr)<<recPtrShift | int64(cnt)
}
func recNext(rec int64) int { return int(uint64(rec) >> recNextShift) }
func recPtr(rec int64) int  { return int(int32(uint64(rec)>>recPtrShift)) & recPtrMask }
func recCnt(rec int64) int  { return int(int32(rec)) & recCntMask }

func (h *histIndex) hashOf(s seq, idx int) int {
	return int(uint32(s.hash(idx)) * 0x9e370001 >> h.keyShift)
}

func (h *histIndex) findLongestCommonSequence() *Edit {
	if !h.scanA() {
		return nil
	}
	h.lcs = &Edit{}
	h.cnt = maxChainLength + 1
	for bPtr := h.region.BeginB; bPtr < h.region.EndB; {
		bPtr = h.tryLongestCommonSequence(bPtr)
	}
	if h.hasCommon && maxChainLength < h.cnt {
		return nil
	}
	return h.lcs
}

func (h *histIndex) scanA() bool {
scan:
	for ptr := h.region.EndA - 1; h.region.BeginA <= ptr; ptr-- {
		tIdx := h.hashOf(h.a, ptr)
		chainLen := 0
		for rIdx := int(h.table[tIdx]); rIdx != 0; {
			rec := h.recs[rIdx]
			if seqEquals(h.a, recPtr(rec), h.a, ptr) {
				newCnt := recCnt(rec) + 1
				if maxCnt < newCnt {
					newCnt = maxCnt
				}
				h.recs[rIdx] = recCreate(recNext(rec), ptr, newCnt)
				h.next[ptr-h.ptrShift] = int32(recPtr(rec))
				h.recIdx[ptr-h.ptrShift] = int32(rIdx)
				continue scan
			}
			rIdx = recNext(rec)
			chainLen++
		}
		if chainLen == maxChainLength {
			return false
		}
		h.recCnt++
		rIdx := h.recCnt
		if rIdx == len(h.recs) {
			sz := len(h.recs) << 1
			if m := 1 + h.region.lenA(); m < sz {
				sz = m
			}
			n := make([]int64, sz)
			copy(n, h.recs)
			h.recs = n
		}
		h.recs[rIdx] = recCreate(int(h.table[tIdx]), ptr, 1)
		h.recIdx[ptr-h.ptrShift] = int32(rIdx)
		h.table[tIdx] = int32(rIdx)
	}
	return true
}

func (h *histIndex) tryLongestCommonSequence(bPtr int) int {
	bNext := bPtr + 1
	rIdx := int(h.table[h.hashOf(h.b, bPtr)])
	for rIdx != 0 {
		rec := h.recs[rIdx]
		nextIdx := recNext(rec)
		if recCnt(rec) > h.cnt {
			if !h.hasCommon {
				h.hasCommon = seqEquals(h.a, recPtr(rec), h.b, bPtr)
			}
			rIdx = nextIdx
			continue
		}
		as := recPtr(rec)
		if !seqEquals(h.a, as, h.b, bPtr) {
			rIdx = nextIdx
			continue
		}
		h.hasCommon = true
	tryLocations:
		for {
			np := int(h.next[as-h.ptrShift])
			bs := bPtr
			ae := as + 1
			be := bs + 1
			rc := recCnt(rec)
			for h.region.BeginA < as && h.region.BeginB < bs && seqEquals(h.a, as-1, h.b, bs-1) {
				as--
				bs--
				if 1 < rc {
					rc = min(rc, recCnt(h.recs[h.recIdx[as-h.ptrShift]]))
				}
			}
			for ae < h.region.EndA && be < h.region.EndB && seqEquals(h.a, ae, h.b, be) {
				if 1 < rc {
					rc = min(rc, recCnt(h.recs[h.recIdx[ae-h.ptrShift]]))
				}
				ae++
				be++
			}
			if bNext < be {
				bNext = be
			}
			if h.lcs.lenA() < ae-as || rc < h.cnt {
				h.lcs.BeginA = as
				h.lcs.BeginB = bs
				h.lcs.EndA = ae
				h.lcs.EndB = be
				h.cnt = rc
			}
			if np == 0 {
				break tryLocations
			}
			for np < ae {
				np = int(h.next[np-h.ptrShift])
				if np == 0 {
					break tryLocations
				}
			}
			as = np
		}
		rIdx = nextIdx
	}
	return bNext
}
