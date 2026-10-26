package canary

import (
	"fmt"
	"math/big"
	"math/rand/v2"
	"strconv"
	"strings"
)

// Parameterized reasoning tasks. Every round derives one instance from its
// sequence number, so all accounts of a round get the same question, while the
// concrete numbers change between rounds (no fixed public answer to memorise).
// Answers are computed locally.

const finalAnswerSuffix = " Think carefully, then end your reply with a final line exactly in the form `Final answer: <integer>`."

type generator struct {
	id   string
	make func(r *rand.Rand) (text string, answer string)
}

func between(r *rand.Rand, lo, hi int) int { return lo + r.IntN(hi-lo+1) }

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	if a < 0 {
		return -a
	}
	return a
}

func binom(n, k int) *big.Int { return new(big.Int).Binomial(int64(n), int64(k)) }

var generators = []generator{
	{"modpow", func(r *rand.Rand) (string, string) {
		base := []int{3, 7, 11, 13, 17, 19, 23}[r.IntN(7)]
		exp := between(r, 1000, 9999)
		mod := []int{1000, 997, 1009, 10000}[r.IntN(4)]
		ans := new(big.Int).Exp(big.NewInt(int64(base)), big.NewInt(int64(exp)), big.NewInt(int64(mod)))
		return fmt.Sprintf("What is the remainder when %d^%d is divided by %d?", base, exp, mod), ans.String()
	}},
	{"incl-excl", func(r *rand.Rand) (string, string) {
		n := between(r, 500, 5000)
		ps := []int{2, 3, 5, 7, 11, 13}
		r.Shuffle(len(ps), func(i, j int) { ps[i], ps[j] = ps[j], ps[i] })
		a, b, c := ps[0], ps[1], ps[2]
		count := 0
		for i := 1; i <= n; i++ {
			if (i%a == 0 || i%b == 0) && i%c != 0 {
				count++
			}
		}
		return fmt.Sprintf("How many positive integers n with 1 <= n <= %d are divisible by %d or by %d but not divisible by %d?", n, a, b, c), strconv.Itoa(count)
	}},
	{"domino-2xn", func(r *rand.Rand) (string, string) {
		n := between(r, 8, 30)
		a, b := big.NewInt(1), big.NewInt(1)
		for i := 2; i <= n; i++ {
			a, b = b, new(big.Int).Add(a, b)
		}
		return fmt.Sprintf("In how many ways can a 2 x %d rectangle be tiled completely with 1 x 2 dominoes (rotations allowed, tilings that differ only by symmetry count as different)?", n), b.String()
	}},
	{"domino-3xn", func(r *rand.Rand) (string, string) {
		n := 2 * between(r, 3, 9)
		f := []int64{1, 3}
		for len(f) <= n/2 {
			k := len(f)
			f = append(f, 4*f[k-1]-f[k-2])
		}
		return fmt.Sprintf("In how many ways can a 3 x %d rectangle be tiled completely with 1 x 2 dominoes (rotations allowed, tilings that differ only by symmetry count as different)?", n), strconv.FormatInt(f[n/2], 10)
	}},
	{"tile-2xn-squares", func(r *rand.Rand) (string, string) {
		n := between(r, 8, 25)
		a, b := int64(1), int64(1)
		for i := 2; i <= n; i++ {
			a, b = b, b+2*a
		}
		return fmt.Sprintf("In how many ways can a 2 x %d rectangle be tiled completely using 1 x 2 dominoes (in either orientation) and 2 x 2 squares?", n), strconv.FormatInt(b, 10)
	}},
	{"lattice-blocked", func(r *rand.Rand) (string, string) {
		w, h := between(r, 6, 10), between(r, 6, 10)
		bx, by := between(r, 1, w-1), between(r, 1, h-1)
		total := binom(w+h, w)
		through := new(big.Int).Mul(binom(bx+by, bx), binom(w-bx+h-by, w-bx))
		return fmt.Sprintf("A path on the integer grid goes from (0,0) to (%d,%d) using only unit steps right (+1,0) or up (0,+1). How many such paths do not pass through the point (%d,%d)?", w, h, bx, by), new(big.Int).Sub(total, through).String()
	}},
	{"digit-sum-pow2", func(r *rand.Rand) (string, string) {
		k := between(r, 60, 160)
		sum := 0
		for _, ch := range new(big.Int).Lsh(big.NewInt(1), uint(k)).String() {
			sum += int(ch - '0')
		}
		return fmt.Sprintf("What is the sum of the decimal digits of 2^%d?", k), strconv.Itoa(sum)
	}},
	{"coprime-count", func(r *rand.Rand) (string, string) {
		n := between(r, 300, 3000)
		m := []int{30, 42, 60, 66, 70, 84, 90, 105, 210}[r.IntN(9)]
		count := 0
		for i := 1; i <= n; i++ {
			if gcd(i, m) == 1 {
				count++
			}
		}
		return fmt.Sprintf("How many integers n with 1 <= n <= %d satisfy gcd(n, %d) = 1?", n, m), strconv.Itoa(count)
	}},
	{"divisor-count", func(r *rand.Rand) (string, string) {
		n := between(r, 100000, 999999)
		count := 0
		for d := 1; d*d <= n; d++ {
			if n%d == 0 {
				count += 2
				if d*d == n {
					count--
				}
			}
		}
		return fmt.Sprintf("How many positive divisors does %d have?", n), strconv.Itoa(count)
	}},
	{"no-three-ones", func(r *rand.Rand) (string, string) {
		n := between(r, 10, 24)
		count := 0
		for mask := 0; mask < 1<<n; mask++ {
			if mask&(mask>>1)&(mask>>2) == 0 {
				count++
			}
		}
		return fmt.Sprintf("How many binary strings of length %d contain no three consecutive 1s?", n), strconv.Itoa(count)
	}},
	{"bounded-solutions", func(r *rand.Rand) (string, string) {
		k := between(r, 8, 20)
		n := between(r, k, 2*k)
		count := 0
		for x := 0; x <= k; x++ {
			for y := 0; y <= k; y++ {
				if z := n - x - y; z >= 0 && z <= k {
					count++
				}
			}
		}
		return fmt.Sprintf("How many ordered triples of integers (x, y, z) satisfy x + y + z = %d with 0 <= x, y, z <= %d?", n, k), strconv.Itoa(count)
	}},
	{"prime-sum", func(r *rand.Rand) (string, string) {
		n := between(r, 200, 1500)
		sieve := make([]bool, n)
		sum := 0
		for i := 2; i < n; i++ {
			if !sieve[i] {
				sum += i
				for j := i * i; j < n; j += i {
					sieve[j] = true
				}
			}
		}
		return fmt.Sprintf("What is the sum of all prime numbers less than %d?", n), strconv.Itoa(sum)
	}},
	{"factorial-zeros", func(r *rand.Rand) (string, string) {
		n := between(r, 200, 9999)
		count := 0
		for p := 5; p <= n; p *= 5 {
			count += n / p
		}
		return fmt.Sprintf("How many trailing zeros does %d! (the factorial of %d) have in base 10?", n, n), strconv.Itoa(count)
	}},
	{"crt", func(r *rand.Rand) (string, string) {
		mods := [][3]int{{7, 11, 13}, {5, 9, 13}, {8, 9, 11}, {7, 9, 17}, {5, 11, 19}}[r.IntN(5)]
		res := [3]int{r.IntN(mods[0]), r.IntN(mods[1]), r.IntN(mods[2])}
		limit := mods[0] * mods[1] * mods[2]
		x := limit
		for v := 1; v <= limit; v++ {
			if v%mods[0] == res[0] && v%mods[1] == res[1] && v%mods[2] == res[2] {
				x = v
				break
			}
		}
		return fmt.Sprintf("What is the smallest positive integer x such that x leaves remainder %d when divided by %d, remainder %d when divided by %d, and remainder %d when divided by %d?", res[0], mods[0], res[1], mods[1], res[2], mods[2]), strconv.Itoa(x)
	}},
}

// GeneratorIDs lists the parameterized task families.
func GeneratorIDs() []string {
	out := make([]string, len(generators))
	for i, g := range generators {
		out[i] = g.id
	}
	return out
}

// Generate builds the reasoning instance for a round sequence (deterministic).
func Generate(sequence int64) Prompt {
	if sequence < 0 {
		sequence = -sequence
	}
	g := generators[sequence%int64(len(generators))]
	return generateWith(g, sequence)
}

func generateWith(g generator, sequence int64) Prompt {
	r := rand.New(rand.NewPCG(uint64(sequence), 0x6772_6f6b_3241_5049))
	text, answer := g.make(r)
	return Prompt{ID: "gen-" + g.id + "-" + strconv.FormatInt(sequence, 10), Kind: KindReasoning, Text: text + finalAnswerSuffix, Expected: answer}
}

// generatedByID rebuilds a generated prompt from its id ("gen-<family>-<seq>").
func generatedByID(id string) (Prompt, bool) {
	if !strings.HasPrefix(id, "gen-") {
		return Prompt{}, false
	}
	cut := strings.LastIndexByte(id, '-')
	if cut <= 4 {
		return Prompt{}, false
	}
	family, seqText := id[4:cut], id[cut+1:]
	sequence, err := strconv.ParseInt(seqText, 10, 64)
	if err != nil {
		return Prompt{}, false
	}
	for _, g := range generators {
		if g.id == family {
			return generateWith(g, sequence), true
		}
	}
	return Prompt{}, false
}
