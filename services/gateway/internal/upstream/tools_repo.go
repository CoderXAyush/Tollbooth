package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolsRepository handles persistence of cached upstream tool definitions in PostgreSQL.
type ToolsRepository interface {
	SaveServerTools(ctx context.Context, serverID uuid.UUID, tools []*mcp.Tool) error
	GetServerTools(ctx context.Context, serverID uuid.UUID) ([]*mcp.Tool, error)
	DeleteServerTools(ctx context.Context, serverID uuid.UUID) error
}

// PostgresToolsRepository implements ToolsRepository using pgxpool.
type PostgresToolsRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresToolsRepository creates a new PostgresToolsRepository.
func NewPostgresToolsRepository(pool *pgxpool.Pool) *PostgresToolsRepository {
	return &PostgresToolsRepository{pool: pool}
}

// SaveServerTools synchronizes the server's tools in the database in a transaction.
func (r *PostgresToolsRepository) SaveServerTools(ctx context.Context, serverID uuid.UUID, tools []*mcp.Tool) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Delete existing tools for this server to keep cache fresh
	if _, err := tx.Exec(ctx, "DELETE FROM mcp_tools WHERE server_id = $1", serverID); err != nil {
		return fmt.Errorf("failed to delete old tools: %w", err)
	}

	insertQuery := `
		INSERT INTO mcp_tools (server_id, name, description, input_schema, annotations, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (server_id, name) DO UPDATE
		SET description = EXCLUDED.description,
		    input_schema = EXCLUDED.input_schema,
		    annotations = EXCLUDED.annotations,
		    updated_at = EXCLUDED.updated_at;
	`
	now := time.Now().UTC()

	for _, t := range tools {
		inputSchemaJSON, err := json.Marshal(t.InputSchema)
		if err != nil || len(inputSchemaJSON) == 0 {
			inputSchemaJSON = []byte(`{"type":"object"}`)
		}

		annotationsJSON, err := json.Marshal(t.Annotations)
		if err != nil || len(annotationsJSON) == 0 || string(annotationsJSON) == "null" {
			annotationsJSON = []byte(`{}`)
		}

		if _, err := tx.Exec(ctx, insertQuery, serverID, t.Name, t.Description, inputSchemaJSON, annotationsJSON, now); err != nil {
			return fmt.Errorf("failed to insert tool %q: %w", t.Name, err)
		}
	}

	return tx.Commit(ctx)
}

// GetServerTools retrieves cached tools for a given server ID.
func (r *PostgresToolsRepository) GetServerTools(ctx context.Context, serverID uuid.UUID) ([]*mcp.Tool, error) {
	query := `
		SELECT name, description, input_schema, annotations
		FROM mcp_tools
		WHERE server_id = $1
		ORDER BY name ASC;
	`
	rows, err := r.pool.Query(ctx, query, serverID)
	if err != nil {
		return nil, fmt.Errorf("failed to query mcp_tools: %w", err)
	}
	defer rows.Close()

	tools := make([]*mcp.Tool, 0)
	for rows.Next() {
		var name string
		var desc *string
		var inputSchemaBytes, annotationsBytes []byte

		if err := rows.Scan(&name, &desc, &inputSchemaBytes, &annotationsBytes); err != nil {
			return nil, fmt.Errorf("failed to scan tool: %w", err)
		}

		t := &mcp.Tool{
			Name: name,
		}
		if desc != nil {
			t.Description = *desc
		}
		if len(inputSchemaBytes) > 0 {
			t.InputSchema = json.RawMessage(inputSchemaBytes)
		} else {
			t.InputSchema = json.RawMessage(`{"type":"object"}`)
		}
		if len(annotationsBytes) > 0 && string(annotationsBytes) != "{}" && string(annotationsBytes) != "null" {
			var annot mcp.ToolAnnotations
			if err := json.Unmarshal(annotationsBytes, &annot); err == nil {
				t.Annotations = &annot
			}
		}

		tools = append(tools, t)
	}

	return tools, rows.Err()
}

// DeleteServerTools removes all tools for a server.
func (r *PostgresToolsRepository) DeleteServerTools(ctx context.Context, serverID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, "DELETE FROM mcp_tools WHERE server_id = $1", serverID)
	return err
}
