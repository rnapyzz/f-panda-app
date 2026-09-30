package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireRole(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := RequireRole(RoleFPAAdmin, RoleManager)(ok)

	tests := []struct {
		name string
		user *User
		want int
	}{
		{name: "未ログイン", user: nil, want: http.StatusUnauthorized},
		{name: "許可されたロール", user: &User{ID: 1, Role: RoleManager}, want: http.StatusOK},
		{name: "許可されていないロール", user: &User{ID: 2, Role: RoleViewer}, want: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tt.user != nil {
				req = req.WithContext(WithUser(req.Context(), *tt.user))
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}
