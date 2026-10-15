package dashboard

import "testing"

func TestPercentileMS(t *testing.T) {
	values := []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if PercentileMS(values, 50) != 5 || PercentileMS(values, 90) != 9 || PercentileMS(nil, 50) != 0 || PercentileMS([]int64{7}, 90) != 7 {
		t.Fatal("unexpected percentile")
	}
}
