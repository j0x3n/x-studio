package assembly

import (
	"math"
	"testing"
)

func TestCalcRequiredQty(t *testing.T) {
	tests := []struct {
		name     string
		qty      int64
		runs     int64
		lossBps  int64
		want     int64
		wantErr  bool
	}{
		{"exact without loss", 10, 2, 0, 20, false},
		{"with 5 percent loss ceil", 3, 1, 500, 4, false},       // 3 * 1.05 = 3.15 -> 4
		{"exact with 50 percent loss", 2, 1, 5000, 3, false},    // 2 * 1.50 = 3 -> 3
		{"minimal loss bps ceil", 1, 1, 1, 2, false},            // 1 * 1.0001 = 1.0001 -> 2
		{"zero quantity invalid", 0, 1, 100, 0, true},
		{"negative quantity invalid", -5, 1, 100, 0, true},
		{"zero runs invalid", 5, 0, 100, 0, true},
		{"negative runs invalid", 5, -1, 100, 0, true},
		{"loss bps negative invalid", 5, 1, -1, 0, true},
		{"loss bps over 10000 invalid", 5, 1, 10001, 0, true},
		{"overflow protection", math.MaxInt64, 2, 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := calcRequiredQty(tt.qty, tt.runs, tt.lossBps)
			if (err != nil) != tt.wantErr {
				t.Fatalf("calcRequiredQty() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("calcRequiredQty() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalcTotalLabor(t *testing.T) {
	tests := []struct {
		name      string
		laborCost int64
		runs      int64
		want      int64
		wantErr   bool
	}{
		{"zero labor cost", 0, 10, 0, false},
		{"normal labor cost", 250, 4, 1000, false},
		{"negative labor invalid", -1, 1, 0, true},
		{"zero runs invalid", 100, 0, 0, true},
		{"overflow protection", math.MaxInt64, 2, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := calcTotalLabor(tt.laborCost, tt.runs)
			if (err != nil) != tt.wantErr {
				t.Fatalf("calcTotalLabor() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("calcTotalLabor() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalcTotalOutputQty(t *testing.T) {
	tests := []struct {
		name      string
		outputQty int64
		runs      int64
		want      int64
		wantErr   bool
	}{
		{"normal output qty", 5, 3, 15, false},
		{"zero output qty invalid", 0, 3, 0, true},
		{"negative output qty invalid", -5, 3, 0, true},
		{"zero runs invalid", 5, 0, 0, true},
		{"overflow protection", math.MaxInt64, 2, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := calcTotalOutputQty(tt.outputQty, tt.runs)
			if (err != nil) != tt.wantErr {
				t.Fatalf("calcTotalOutputQty() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("calcTotalOutputQty() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSumOverflow(t *testing.T) {
	_, err := sum(math.MaxInt64, 1)
	if err == nil {
		t.Fatal("expected overflow error for MaxInt64 + 1")
	}

	v, err := sum(100, 200)
	if err != nil || v != 300 {
		t.Fatalf("expected 300, got %d, err %v", v, err)
	}
}
