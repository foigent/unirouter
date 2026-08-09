package admin

import (
	"net/http/httptest"
	"testing"
)

func TestWorkloadCredentialPaginationIsStrictlyBounded(t *testing.T) {
	tests := []struct {
		query       string
		page        int
		pageSize    int
		expectError bool
	}{
		{"", 1, defaultPageSize, false},
		{"?page=2&page_size=100", 2, 100, false},
		{"?page=0", 0, 0, true},
		{"?page=1001", 0, 0, true},
		{"?page=not-a-number", 0, 0, true},
		{"?page_size=0", 0, 0, true},
		{"?page_size=101", 0, 0, true},
		{"?page_size=not-a-number", 0, 0, true},
	}
	for _, test := range tests {
		t.Run(test.query, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/api/v1/workload-applications/app/credentials"+test.query, nil)
			page, pageSize, err := parseBoundedWorkloadCredentialPagination(request)
			if (err != nil) != test.expectError || page != test.page || pageSize != test.pageSize {
				t.Fatalf("page=%d page_size=%d err=%v", page, pageSize, err)
			}
		})
	}
}
