package internal

import (
	"crypto/rand"
	"math"
	"math/big"
)

// GenId generates a unique identifier as a string
func GenId() string {
	n, err := rand.Int(rand.Reader, big.NewInt(math.MaxInt64))
	if err != nil {
		panic(err)
	}

	return n.String()
}
