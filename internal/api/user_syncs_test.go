package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mattjackson/basement/internal/config"
	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/sync"
	"github.com/mattjackson/basement/internal/store"
)

func TestUserCreateSyncHandler(t *testing.T) {
	cfg := &config.Config{
		DataDir: t.TempDir(),
		JWT: config.JWTConfig{
			Secret: "test-secret",
		},
	}

	connStore, err := store.OpenConnections(cfg.DataDir)
	if err != nil {
		t.Fatalf("OpenConnections() error = %v", err)
	}

	st, err := store.Open(cfg.DataDir, 0)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}

	reg := driver.NewRegistry()
	srv := New(cfg, st, connStore, nil, reg)

	tests := []struct {
		name           string
		body           map[string]interface{}
		setupUser      bool
		srcCluster     bool
		dstCluster     bool
		expectedStatus int
	}{
		{
			name: "valid pull sync",
			body: map[string]interface{}{
				"mode":            "pull",
				"srcConnectionId": "conn-1",
				"srcBucket":       "bucket-a",
				"dstConnectionId": "conn-2",
				"dstBucket":       "bucket-b",
			},
			setupUser:      true,
			srcCluster:     true,
			dstCluster:     true,
			expectedStatus: http.StatusAccepted,
		},
		{
			name: "invalid mode (push not supported)",
			body: map[string]interface{}{
				"mode":            "push",
				"srcConnectionId": "conn-1",
				"srcBucket":       "bucket-a",
				"dstConnectionId": "conn-2",
				"dstBucket":       "bucket-b",
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "missing source connection",
			body: map[string]interface{}{
				"mode":            "pull",
				"srcConnectionId": "",
				"srcBucket":       "bucket-a",
				"dstConnectionId": "conn-2",
				"dstBucket":       "bucket-b",
			},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test data
			if tt.setupUser {
				st.CreateUser(&store.User{ID: "user-1", Email: "test@example.com"})
				connStore.Create(nil, store.Connection{
					ID:     "conn-1",
					Label:  "Source Cluster",
					Driver: "garage-v1",
					Config: map[string]string{"adminUrl": "http://localhost:7878"},
				})
			}

			if tt.dstCluster {
				connStore.Create(nil, store.Connection{
					ID:     "conn-2",
					Label:  "Dest Cluster",
					Driver: "garage-v1",
					Config: map[string]string{"adminUrl": "http://localhost:7879"},
				})
			}

			bodyBytes, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/user/syncs", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")

			if tt.setupUser {
				// Add JWT cookie for authenticated user
				token, _ := st.CreateToken("user-1", 24*time.Hour)
				req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: token})
			}

			w := httptest.NewRecorder()
			srv.router.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d. Body: %s", tt.expectedStatus, w.Code, w.Body.String())
			}

			if tt.expectedStatus == http.StatusAccepted {
				var resp map[string]interface{}
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Errorf("Failed to decode response: %v", err)
				} else if resp["state"] != "queued" {
					t.Errorf("Expected state 'queued', got '%v'", resp["state"])
				}
			}
		})
	}
}

func TestUserListSyncsHandler(t *testing.T) {
	cfg := &config.Config{
		DataDir: t.TempDir(),
		JWT: config.JWTConfig{
			Secret: "test-secret",
		},
	}

	connStore, err := store.OpenConnections(cfg.DataDir)
	if err != nil {
		t.Fatalf("OpenConnections() error = %v", err)
	}

	st, err := store.Open(cfg.DataDir, 0)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}

	reg := driver.NewRegistry()
	srv := New(cfg, st, connStore, nil, reg)

	// Create test user and sync jobs
	st.CreateUser(&store.User{ID: "user-1", Email: "test@example.com"})
	connStore.Create(nil, store.Connection{
		ID:     "conn-1",
		Label:  "Cluster 1",
		Driver: "garage-v1",
	})

	syncJob := &sync.SyncJob{
		ID:              sync.GenerateID(),
		OwnerUserID:     "user-1",
		Mode:            "pull",
		SrcConnectionID: "conn-1",
		SrcBucket:       "bucket-a",
		DstConnectionID: "conn-1",
		DstBucket:       "bucket-b",
		CreatedAt:       st.Now(),
		State:           "queued",
	}

	syncStore := sync.NewFileStore(cfg.DataDir)
	if err := syncStore.Save(syncJob); err != nil {
		t.Fatalf("syncStore.Save() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/syncs", nil)
	token, _ := st.CreateToken("user-1", 24*time.Hour)
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: token})

	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var jobs []sync.SyncJob
	if err := json.NewDecoder(w.Body).Decode(&jobs); err != nil {
		t.Errorf("Failed to decode response: %v", err)
	} else if len(jobs) != 1 {
		t.Errorf("Expected 1 sync job, got %d", len(jobs))
	}
}

func TestUserGetSyncHandler(t *testing.T) {
	cfg := &config.Config{
		DataDir: t.TempDir(),
		JWT: config.JWTConfig{
			Secret: "test-secret",
		},
	}

	connStore, err := store.OpenConnections(cfg.DataDir)
	if err != nil {
		t.Fatalf("OpenConnections() error = %v", err)
	}

	st, err := store.Open(cfg.DataDir, 0)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}

	reg := driver.NewRegistry()
	srv := New(cfg, st, connStore, nil, reg)

	user1 := "user-1"
	user2 := "user-2"
	st.CreateUser(&store.User{ID: user1, Email: "user1@example.com"})
	st.CreateUser(&store.User{ID: user2, Email: "user2@example.com"})
	connStore.Create(nil, store.Connection{
		ID:     "conn-1",
		Label:  "Cluster 1",
		Driver: "garage-v1",
	})

	job := &sync.SyncJob{
		ID:              sync.GenerateID(),
		OwnerUserID:     user1,
		Mode:            "pull",
		SrcConnectionID: "conn-1",
		SrcBucket:       "bucket-a",
		DstConnectionID: "conn-1",
		DstBucket:       "bucket-b",
		CreatedAt:       st.Now(),
		State:           "running",
	}

	syncStore := sync.NewFileStore(cfg.DataDir)
	if err := syncStore.Save(job); err != nil {
		t.Fatalf("syncStore.Save() error = %v", err)
	}

	tests := []struct {
		name           string
		userID         string
		jobID          string
		expectedStatus int
	}{
		{
			name:           "owner can view job",
			userID:         user1,
			jobID:          job.ID,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "non-owner cannot view job",
			userID:         user2,
			jobID:          job.ID,
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "job not found",
			userID:         user1,
			jobID:          "non-existent-id",
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/user/syncs/"+tt.jobID, nil)
			token, _ := st.CreateToken(tt.userID, 24*time.Hour)
			req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: token})

			w := httptest.NewRecorder()
			srv.router.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d. Body: %s", tt.expectedStatus, w.Code, w.Body.String())
			}
		})
	}
}

func TestUserDeleteSyncHandler(t *testing.T) {
	cfg := &config.Config{
		DataDir: t.TempDir(),
		JWT: config.JWTConfig{
			Secret: "test-secret",
		},
	}

	connStore, err := store.OpenConnections(cfg.DataDir)
	if err != nil {
		t.Fatalf("OpenConnections() error = %v", err)
	}

	st, err := store.Open(cfg.DataDir, 0)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}

	reg := driver.NewRegistry()
	srv := New(cfg, st, connStore, nil, reg)

	user1 := "user-1"
	st.CreateUser(&store.User{ID: user1, Email: "user1@example.com"})
	connStore.Create(nil, store.Connection{
		ID:     "conn-1",
		Label:  "Cluster 1",
		Driver: "garage-v1",
	})

	job := &sync.SyncJob{
		ID:              sync.GenerateID(),
		OwnerUserID:     user1,
		Mode:            "pull",
		SrcConnectionID: "conn-1",
		SrcBucket:       "bucket-a",
		DstConnectionID: "conn-1",
		DstBucket:       "bucket-b",
		CreatedAt:       st.Now(),
		State:           "queued",
	}

	syncStore := sync.NewFileStore(cfg.DataDir)
	if err := syncStore.Save(job); err != nil {
		t.Fatalf("syncStore.Save() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/user/syncs/"+job.ID, nil)
	token, _ := st.CreateToken(user1, 24*time.Hour)
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: token})

	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusNoContent, w.Code, w.Body.String())
	}

	// Verify job is deleted
	_, err = syncStore.Load(job.ID)
	if err == nil {
		t.Error("Expected job to be deleted, but it still exists")
	}
}

func TestUserPauseResumeSyncHandler(t *testing.T) {
	cfg := &config.Config{
		DataDir: t.TempDir(),
		JWT: config.JWTConfig{
			Secret: "test-secret",
		},
	}

	connStore, err := store.OpenConnections(cfg.DataDir)
	if err != nil {
		t.Fatalf("OpenConnections() error = %v", err)
	}

	st, err := store.Open(cfg.DataDir, 0)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}

	reg := driver.NewRegistry()
	srv := New(cfg, st, connStore, nil, reg)

	user1 := "user-1"
	st.CreateUser(&store.User{ID: user1, Email: "user1@example.com"})
	connStore.Create(nil, store.Connection{
		ID:     "conn-1",
		Label:  "Cluster 1",
		Driver: "garage-v1",
	})

	job := &sync.SyncJob{
		ID:              sync.GenerateID(),
		OwnerUserID:     user1,
		Mode:            "pull",
		SrcConnectionID: "conn-1",
		SrcBucket:       "bucket-a",
		DstConnectionID: "conn-1",
		DstBucket:       "bucket-b",
		CreatedAt:       st.Now(),
		State:           "queued",
	}

	syncStore := sync.NewFileStore(cfg.DataDir)
	if err := syncStore.Save(job); err != nil {
		t.Fatalf("syncStore.Save() error = %v", err)
	}

	t.Run("pause queued job", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/user/syncs/"+job.ID+"/pause", nil)
		token, _ := st.CreateToken(user1, 24*time.Hour)
		req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: token})

		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}
	})

	// Resume should work after pause
	t.Run("resume paused job", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/user/syncs/"+job.ID+"/resume", nil)
		token, _ := st.CreateToken(user1, 24*time.Hour)
		req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: token})

		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}
	})
}
