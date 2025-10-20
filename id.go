package websocket

import (
	"crypto/rand"
	"math"
	"math/big"
)

func GenId() string {
	n, err := rand.Int(rand.Reader, big.NewInt(math.MaxInt64))
	if err != nil {
		panic(err)
	}
	return n.String()
}
