package rng

import (
	"sealdice-core/dice/rng/rng"
	"sealdice-core/dice/rng/sequential"
	"sealdice-core/dice/rng/uniform"
)

var allRNGs = []rng.DiceRng{
	uniform.RNG,
	sequential.RNG,
}

var SelectedRNG rng.DiceRng = uniform.RNG
