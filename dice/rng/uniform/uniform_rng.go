package uniform

import (
	"math/rand"
	"sealdice-core/dice/rng/rng"
)

type uniformRng struct{}

func (receiver *uniformRng) DiceRoll(numFace int) int {
	if numFace <= 0 {
		return 0
	}
	return rand.Int()%numFace + 1
}

func (receiver *uniformRng) DiceRoll64(numFace int64) int64 {
	if numFace <= 0 {
		return 0
	}
	return rand.Int63()%numFace + 1
}

var RNG rng.DiceRng = &uniformRng{}
