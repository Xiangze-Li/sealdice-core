package sequential

import "sealdice-core/dice/rng/rng"

type sequentialRng struct {
	lastRoll map[int64]int64
}

func (receiver *sequentialRng) DiceRoll(numFace int) int {
	if numFace <= 0 {
		return 0
	}

	return int(receiver.DiceRoll64(int64(numFace)))
}

func (receiver *sequentialRng) DiceRoll64(numFace int64) int64 {
	if numFace <= 0 {
		return 0
	}

	ret := receiver.lastRoll[numFace]%numFace + 1
	receiver.lastRoll[numFace]++
	return ret
}

var RNG rng.DiceRng = &sequentialRng{make(map[int64]int64)}
