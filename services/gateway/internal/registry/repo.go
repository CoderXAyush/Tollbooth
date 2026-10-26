package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/CoderXAyush/Tollbooth/services/gateway/internal/crypto"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository manages database persistence for MCP upstream servers.
type Repository interface {
	Create(ctx context.Context, server *Server, cred *UpstreamCredential) (*Server, error)
	GetByID(ctx context.Context, id uuid.UUID) (*Server, error)
	GetBySlug(ctx context.Context, slug string) (*Server, error)
	List(ctx context.Context) ([]*Server, error)
	Update(ctx context.Context, id uuid.UUID, req UpdateServerRequest, cred *UpstreamCredential, credProvided bool) (*Server, error)
	Delete(ctx context.Context, id uuid.UUID) error
	GetDecryptedCredential(ctx context.Context, id uuid.UUID) (*UpstreamCredential, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status string, lastErr *string, consecutiveFails int) error
}

// PostgresRepository implements Repository using PostgreSQL and AES-256-GCM encryption.
type PostgresRepository struct {
	pool      *pgxpool.Pool
	encryptor crypto.Encryptor
}

// NewPostgresRepository creates a new PostgresRepository.
func NewPostgresRepository(pool *pgxpool.Pool, encryptor crypto.Encryptor) *PostgresRepository {
	return &PostgresRepository{
		pool:      pool,
		encryptor: encryptor,
	}
}

// Create inserts a new upstream server, encrypting credentials if provided.
func (r *PostgresRepository) Create(ctx context.Context, s *Server, cred *UpstreamCredential) (*Server, error) {
	if err := ValidateSlug(s.Slug); err != nil {
		return nil, err
	}
	if err := ValidateURL(s.URL); err != nil {
		return nil, err
	}
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" {
		return nil, errors.New("name cannot be empty")
	}

	if s.TimeoutMS <= 0 {
		s.TimeoutMS = 30000
	}

	var credEnc, credNonce []byte
	if cred != nil {
		credBytes, err := json.Marshal(cred)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize credential: %w", err)
		}
		credEnc, credNonce, err = r.encryptor.Encrypt(credBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt credential: %w", err)
		}
	}

	initialStatus := s.Status
	if initialStatus == "" {
		if !s.Enabled {
			initialStatus = "disabled"
		} else {
			initialStatus = "unknown"
		}
	}

	now := time.Now().UTC()
	query := `
		INSERT INTO mcp_servers (
			slug, name, url, enabled,
			credential_enc, credential_nonce,
			status, timeout_ms, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4,
			$5, $6,
			$7, $8, $9, $10
		)
		RETURNING id, created_at, updated_at;
	`

	var id uuid.UUID
	var createdAt, updatedAt time.Time
	err := r.pool.QueryRow(ctx, query,
		s.Slug, s.Name, s.URL, s.Enabled,
		credEnc, credNonce,
		initialStatus, s.TimeoutMS, now, now,
	).Scan(&id, &createdAt, &updatedAt)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // Unique violation
			return nil, ErrSlugAlreadyExists
		}
		return nil, fmt.Errorf("failed to insert server: %w", err)
	}

	return &Server{
		ID:               id,
		Slug:             s.Slug,
		Name:             s.Name,
		URL:              s.URL,
		Enabled:          s.Enabled,
		HasCredential:    len(credEnc) > 0,
		Status:           initialStatus,
		TimeoutMS:        s.TimeoutMS,
		ConsecutiveFails: 0,
		CreatedAt:        createdAt,
		UpdatedAt:        updatedAt,
	}, nil
}

// GetByID retrieves an upstream server by its unique ID.
func (r *PostgresRepository) GetByID(ctx context.Context, id uuid.UUID) (*Server, error) {
	query := `
		SELECT s.id, s.slug, s.name, s.url, s.enabled,
		       s.credential_enc IS NOT NULL, s.status, s.last_error,
		       s.last_checked_at, s.consecutive_fails, s.timeout_ms,
		       s.created_at, s.updated_at,
		       COUNT(t.id) as tool_count
		FROM mcp_servers s
		LEFT JOIN mcp_tools t ON t.server_id = s.id
		WHERE s.id = $1
		GROUP BY s.id;
	`
	return r.scanServer(r.pool.QueryRow(ctx, query, id))
}

// GetBySlug retrieves an upstream server by its slug.
func (r *PostgresRepository) GetBySlug(ctx context.Context, slug string) (*Server, error) {
	query := `
		SELECT s.id, s.slug, s.name, s.url, s.enabled,
		       s.credential_enc IS NOT NULL, s.status, s.last_error,
		       s.last_checked_at, s.consecutive_fails, s.timeout_ms,
		       s.created_at, s.updated_at,
		       COUNT(t.id) as tool_count
		FROM mcp_servers s
		LEFT JOIN mcp_tools t ON t.server_id = s.id
		WHERE s.slug = $1
		GROUP BY s.id;
	`
	return r.scanServer(r.pool.QueryRow(ctx, query, slug))
}

// List retrieves all upstream servers.
func (r *PostgresRepository) List(ctx context.Context) ([]*Server, error) {
	query := `
		SELECT s.id, s.slug, s.name, s.url, s.enabled,
		       s.credential_enc IS NOT NULL, s.status, s.last_error,
		       s.last_checked_at, s.consecutive_fails, s.timeout_ms,
		       s.created_at, s.updated_at,
		       COUNT(t.id) as tool_count
		FROM mcp_servers s
		LEFT JOIN mcp_tools t ON t.server_id = s.id
		GROUP BY s.id
		ORDER BY s.created_at ASC;
	`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query servers: %w", err)
	}
	defer rows.Close()

	servers := make([]*Server, 0)
	for rows.Next() {
		s := &Server{}
		var hasCred bool
		err := rows.Scan(
			&s.ID, &s.Slug, &s.Name, &s.URL, &s.Enabled,
			&hasCred, &s.Status, &s.LastError,
			&s.LastCheckedAt, &s.ConsecutiveFails, &s.TimeoutMS,
			&s.CreatedAt, &s.UpdatedAt, &s.ToolCount,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan server row: %w", err)
		}
		s.HasCredential = hasCred
		servers = append(servers, s)
	}

	return servers, rows.Err()
}

// Update modifies an existing server's properties.
func (r *PostgresRepository) Update(ctx context.Context, id uuid.UUID, req UpdateServerRequest, cred *UpstreamCredential, credProvided bool) (*Server, error) {
	existing, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	name := existing.Name
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, errors.New("name cannot be empty")
		}
	}

	serverURL := existing.URL
	if req.URL != nil {
		if err := ValidateURL(*req.URL); err != nil {
			return nil, err
		}
		serverURL = strings.TrimSpace(*req.URL)
	}

	enabled := existing.Enabled
	status := existing.Status
	if req.Enabled != nil {
		enabled = *req.Enabled
		if !enabled {
			status = "disabled"
		} else if status == "disabled" {
			status = "unknown"
		}
	}

	timeoutMS := existing.TimeoutMS
	if req.TimeoutMS != nil && *req.TimeoutMS > 0 {
		timeoutMS = *req.TimeoutMS
	}

	now := time.Now().UTC()

	if credProvided {
		var credEnc, credNonce []byte
		if cred != nil {
			credBytes, err := json.Marshal(cred)
			if err != nil {
				return nil, fmt.Errorf("failed to serialize credential: %w", err)
			}
			credEnc, credNonce, err = r.encryptor.Encrypt(credBytes)
			if err != nil {
				return nil, fmt.Errorf("failed to encrypt credential: %w", err)
			}
		}

		query := `
			UPDATE mcp_servers
			SET name = $1, url = $2, enabled = $3, status = $4,
			    credential_enc = $5, credential_nonce = $6,
			    timeout_ms = $7, updated_at = $8
			WHERE id = $9
			RETURNING updated_at;
		`
		err = r.pool.QueryRow(ctx, query,
			name, serverURL, enabled, status,
			credEnc, credNonce, timeoutMS, now, id,
		).Scan(&existing.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to update server with credential: %w", err)
		}
		existing.HasCredential = len(credEnc) > 0
	} else {
		query := `
			UPDATE mcp_servers
			SET name = $1, url = $2, enabled = $3, status = $4,
			    timeout_ms = $5, updated_at = $6
			WHERE id = $7
			RETURNING updated_at;
		`
		err = r.pool.QueryRow(ctx, query,
			name, serverURL, enabled, status,
			timeoutMS, now, id,
		).Scan(&existing.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to update server: %w", err)
		}
	}

	existing.Name = name
	existing.URL = serverURL
	existing.Enabled = enabled
	existing.Status = status
	existing.TimeoutMS = timeoutMS

	return existing, nil
}

// Delete removes an upstream server from the registry.
func (r *PostgresRepository) Delete(ctx context.Context, id uuid.UUID) error {
	cmd, err := r.pool.Exec(ctx, "DELETE FROM mcp_servers WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("failed to delete server: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return ErrServerNotFound
	}
	return nil
}

// GetDecryptedCredential fetches and decrypts the upstream credential.
func (r *PostgresRepository) GetDecryptedCredential(ctx context.Context, id uuid.UUID) (*UpstreamCredential, error) {
	var credEnc, credNonce []byte
	err := r.pool.QueryRow(ctx,
		"SELECT credential_enc, credential_nonce FROM mcp_servers WHERE id = $1", id,
	).Scan(&credEnc, &credNonce)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrServerNotFound
		}
		return nil, fmt.Errorf("failed to query credential: %w", err)
	}

	if len(credEnc) == 0 || len(credNonce) == 0 {
		return nil, nil // No credential set
	}

	decryptedBytes, err := r.encryptor.Decrypt(credEnc, credNonce)
	if err != nil {
		return nil, fmt.Errorf("credential decryption error: %w", err)
	}

	var cred UpstreamCredential
	if err := json.Unmarshal(decryptedBytes, &cred); err != nil {
		return nil, fmt.Errorf("failed to deserialize credential: %w", err)
	}

	return &cred, nil
}

// UpdateStatus records a health state change for an upstream server.
func (r *PostgresRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string, lastErr *string, consecutiveFails int) error {
	now := time.Now().UTC()
	query := `
		UPDATE mcp_servers
		SET status = $1, last_error = $2, consecutive_fails = $3,
		    last_checked_at = $4, updated_at = $4
		WHERE id = $5;
	`
	_, err := r.pool.Exec(ctx, query, status, lastErr, consecutiveFails, now, id)
	return err
}

func (r *PostgresRepository) scanServer(row pgx.Row) (*Server, error) {
	s := &Server{}
	var hasCred bool
	err := row.Scan(
		&s.ID, &s.Slug, &s.Name, &s.URL, &s.Enabled,
		&hasCred, &s.Status, &s.LastError,
		&s.LastCheckedAt, &s.ConsecutiveFails, &s.TimeoutMS,
		&s.CreatedAt, &s.UpdatedAt, &s.ToolCount,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrServerNotFound
		}
		return nil, fmt.Errorf("failed to scan server: %w", err)
	}
	s.HasCredential = hasCred
	return s, nil
}
