package ws_client

import (
	"crypto/rand"
	"math"
	"math/big"
)

func genId() string {
	n, err := rand.Int(rand.Reader, big.NewInt(math.MaxInt64))
	if err != nil {
		panic(err)
	}
	return n.String()
}
