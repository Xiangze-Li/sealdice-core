package rng

type DiceRng interface {
	DiceRoll(numFace int) int
	DiceRoll64(numFace int64) int64
}
