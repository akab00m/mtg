package telegram

import (
	"testing"
)

func TestAddressPool_GetRandomDCExcluding(t *testing.T) {
	pool := addressPool{
		v4: productionV4Addresses,
		v6: productionV6Addresses,
	}

	t.Run("excludes specified DC", func(t *testing.T) {
		for exclude := 1; exclude <= 5; exclude++ {
			seen := make(map[int]bool)
			// Run enough iterations to statistically ensure coverage
			for i := 0; i < 100; i++ {
				dc := pool.getRandomDCExcluding(exclude)
				if dc == exclude {
					t.Errorf("getRandomDCExcluding(%d) returned excluded DC", exclude)
				}
				if dc < 1 || dc > 5 {
					t.Errorf("getRandomDCExcluding(%d) returned invalid DC %d", exclude, dc)
				}
				seen[dc] = true
			}
			// Should see all 4 other DCs
			if len(seen) != 4 {
				t.Errorf("getRandomDCExcluding(%d) didn't produce all 4 expected DCs, got %v", exclude, seen)
			}
		}
	})

	t.Run("single DC pool returns that DC", func(t *testing.T) {
		singlePool := addressPool{
			v4: [][]tgAddr{{{network: "tcp4", address: "1.2.3.4:443"}}},
			v6: [][]tgAddr{{{network: "tcp6", address: "[::1]:443"}}},
		}
		dc := singlePool.getRandomDCExcluding(1)
		if dc != 1 {
			t.Errorf("single DC pool should return DC 1, got %d", dc)
		}
	})
}
