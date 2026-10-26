package canary

import (
	"regexp"
	"strconv"
	"testing"
)

func TestGenerateDeterministicAndVaried(t *testing.T) {
	seen := map[string]bool{}
	for seq := int64(0); seq < 200; seq++ {
		a, b := Generate(seq), Generate(seq)
		if a != b {
			t.Fatalf("seq %d not deterministic", seq)
		}
		if a.Kind != KindReasoning || a.Expected == "" || a.Text == "" {
			t.Fatalf("seq %d bad prompt %+v", seq, a)
		}
		if _, err := strconv.ParseInt(a.Expected, 10, 64); err != nil {
			t.Fatalf("seq %d non-integer answer %q", seq, a.Expected)
		}
		if ok, _ := CheckAnswer(a, "work...\nFinal answer: "+a.Expected); !ok {
			t.Fatalf("seq %d own answer rejected", seq)
		}
		got := PromptByID(a.ID)
		if got != a {
			t.Fatalf("PromptByID(%s) mismatch", a.ID)
		}
		seen[a.Text] = true
	}
	if len(seen) < 150 {
		t.Fatalf("too few distinct instances: %d", len(seen))
	}
	if len(GeneratorIDs()) < 10 {
		t.Fatal("need at least 10 generator families")
	}
}

func TestPromptForRoundMixesSVG(t *testing.T) {
	svg := 0
	for seq := int64(0); seq < 20; seq++ {
		if PromptForRound(seq).Kind == KindSVG {
			svg++
		}
	}
	if svg != 4 {
		t.Fatalf("svg rounds = %d", svg)
	}
	if PromptByID("count-3-5-not-7").Expected != "401" {
		t.Fatal("legacy prompt lookup broken")
	}
}

var numRE = regexp.MustCompile(`\d+`)

func nums(text string) []int {
	var out []int
	for _, m := range numRE.FindAllString(text, -1) {
		v, _ := strconv.Atoi(m)
		out = append(out, v)
	}
	return out
}

// Cross-check a few families against independent formulas.
func TestGeneratedAnswersIndependent(t *testing.T) {
	for _, g := range generators {
		for seq := int64(0); seq < 30; seq++ {
			p := generateWith(g, seq)
			n := nums(p.Text)
			want := ""
			switch g.id {
			case "incl-excl":
				N, a, b, c := n[1], n[2], n[3], n[4]
				lcm := func(x, y int) int { return x * y / gcd(x, y) }
				v := N/a + N/b - N/lcm(a, b) - (N/lcm(a, c) + N/lcm(b, c) - N/lcm(lcm(a, b), c))
				want = strconv.Itoa(v)
			case "domino-2xn":
				f := []int{1, 1}
				for len(f) <= n[1] {
					f = append(f, f[len(f)-1]+f[len(f)-2])
				}
				want = strconv.Itoa(f[n[1]])
			case "factorial-zeros":
				v := 0
				for x := n[0]; x > 0; x /= 5 {
					v += x / 5
				}
				want = strconv.Itoa(v)
			case "tile-2xn-squares":
				// Jacobsthal: (2^(n+1) + (-1)^n) / 3
				N := n[1]
				sign := 1
				if N%2 == 1 {
					sign = -1
				}
				want = strconv.Itoa(((1 << (N + 1)) + sign) / 3)
			default:
				continue
			}
			if p.Expected != want {
				t.Fatalf("%s seq %d: %s vs %s (%s)", g.id, seq, p.Expected, want, p.Text)
			}
		}
	}
	if p := (generator{}); p.id != "" {
		t.Fatal()
	}
}
