package telegram

import "math/rand"

type addressPool struct {
	v4 [][]tgAddr
	v6 [][]tgAddr
}

func (a addressPool) isValidDC(dc int) bool {
	// Telegram официально поддерживает только DC 1-5
	// DC > 5 (например, 203) не существуют и должны быть отклонены
	return dc > 0 && dc <= 5 && dc <= len(a.v4) && dc <= len(a.v6)
}

func (a addressPool) getRandomDC() int {
	return 1 + rand.Intn(len(a.v4))
}

// getRandomDCExcluding returns a random DC excluding the specified one.
// Used for fallback when primary DC is unavailable.
func (a addressPool) getRandomDCExcluding(exclude int) int {
	n := len(a.v4)
	if n <= 1 {
		// Only one DC available, return it even if it's the excluded one
		return a.getRandomDC()
	}

	// Pick random from n-1 options, then adjust if we hit the excluded one
	dc := 1 + rand.Intn(n-1)
	if dc >= exclude {
		dc++
	}

	return dc
}

func (a addressPool) getV4(dc int) []tgAddr {
	return a.get(a.v4, dc-1)
}

func (a addressPool) getV6(dc int) []tgAddr {
	return a.get(a.v6, dc-1)
}

func (a addressPool) get(addresses [][]tgAddr, dc int) []tgAddr {
	// Дополнительная проверка: игнорировать DC > 5 (203, 999, и т.д.)
	if dc < 0 || dc >= len(addresses) || dc >= 5 {
		return nil
	}

	rv := make([]tgAddr, len(addresses[dc]))
	copy(rv, addresses[dc])

	if len(rv) > 1 {
		rand.Shuffle(len(rv), func(i, j int) {
			rv[i], rv[j] = rv[j], rv[i]
		})
	}

	return rv
}
