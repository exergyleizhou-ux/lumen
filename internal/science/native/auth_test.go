package native

import "testing"

func TestRequiredAuthCombo(t *testing.T) {
	cases := []struct {
		tool string
		want AuthLevel
	}{
		{"search_datasets", AuthAnonymous},
		{"get_dataset_detail", AuthAnonymous},
		{"preview_schema", AuthUserToken},
		{"list_algorithms", AuthUserToken},
		{"list_offer_signals", AuthAnonymous},
		{"submit_c2d_job", AuthUserToken},
		{"unknown_tool", AuthUserToken},
	}
	for _, tc := range cases {
		if got := RequiredAuth(tc.tool); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.tool, got, tc.want)
		}
	}
}

func TestCheckAuthTokenGated(t *testing.T) {
	if err := CheckAuth("preview_schema", ""); err == nil {
		t.Fatal("preview_schema should require token")
	}
	if err := CheckAuth("list_algorithms", ""); err == nil {
		t.Fatal("list_algorithms should require token")
	}
	if err := CheckAuth("search_datasets", ""); err != nil {
		t.Fatalf("search_datasets should be anonymous: %v", err)
	}
	if err := CheckAuth("preview_schema", "tok"); err != nil {
		t.Fatalf("preview_schema with token: %v", err)
	}
}
