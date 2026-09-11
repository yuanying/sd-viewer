package index

import (
	"context"
	"testing"
)

// facetFields はファセットの各項目を名前つきで並べる。
func facetFields(f *FacetSet) map[string][]FacetValue {
	return map[string][]FacetValue{
		"Models":   f.Models,
		"Loras":    f.Loras,
		"Samplers": f.Samplers,
		"Sizes":    f.Sizes,
		"Dirs":     f.Dirs,
		"Roots":    f.Roots,
	}
}

func TestFacets_該当がなければnilではなく空スライスを返す(t *testing.T) {
	tests := []struct {
		name  string
		newDB func(t *testing.T) *DB
		query Query
	}{
		{"一致しない全文検索", newFixtureDB, Query{Text: "nomatch"}},
		{"Fav が 1 枚もない", newFixtureDB, Query{Fav: true}},
		{"空のインデックス", newTestDB, Query{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			db := tt.newDB(t)

			// When
			facets, err := db.Facets(context.Background(), tt.query)
			if err != nil {
				t.Fatalf("Facets() error = %v", err)
			}

			// Then
			for name, values := range facetFields(facets) {
				if values == nil {
					t.Errorf("%s = nil, want 空スライス", name)
				}
				if len(values) != 0 {
					t.Errorf("%s = %v, want 空", name, values)
				}
			}
		})
	}
}

func TestFacets_LoRAを持つ画像がなければLoRAは空スライス(t *testing.T) {
	// Given: LoRA を持たない画像だけのインデックス
	db := newTestDB(t)
	put(t, db, &Image{Root: "out", Path: "a.png", CreatedAt: fixtureBase, Model: "modelA"})

	// When
	facets, err := db.Facets(context.Background(), Query{})
	if err != nil {
		t.Fatalf("Facets() error = %v", err)
	}

	// Then
	if facets.Loras == nil || len(facets.Loras) != 0 {
		t.Errorf("Loras = %#v, want 空スライス", facets.Loras)
	}
	if got := facetCount(facets.Models, "modelA"); got != 1 {
		t.Errorf("modelA の件数 = %d, want 1", got)
	}
}
