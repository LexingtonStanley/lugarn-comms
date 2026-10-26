package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// InsertRequest inserts an asynchronous runner request.
func (tx *Tx) InsertRequest(p RequestParams) error {
	r := &p.Request
	if r.Status == "" {
		r.Status = model.RequestStatusPending
	}
	r.CreatedAt = nowIfZero(r.CreatedAt)
	if r.Args == nil {
		r.Args = map[string]any{}
	}
	args, err := json.Marshal(r.Args)
	if err != nil {
		return asError(err)
	}
	var errorCode, errorMessage any
	errorDetails := "{}"
	if r.Error != nil {
		errorCode, errorMessage = r.Error.Code, r.Error.Message
		details := r.Error.Details
		if details == nil {
			details = map[string]any{}
		}
		encoded, marshalErr := json.Marshal(details)
		if marshalErr != nil {
			return asError(marshalErr)
		}
		errorDetails = string(encoded)
	}
	_, err = tx.exec(`INSERT INTO requests (id, team_id, kind, member_id, requested_by, args, status,
 error_code, error_message, error_details, created_at, started_at, finished_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Team, r.Kind, dbPtr(p.MemberID), p.RequestedByID, string(args), r.Status,
		errorCode, errorMessage, errorDetails, dbTime(r.CreatedAt), dbTimePtr(r.StartedAt), dbTimePtr(r.FinishedAt))
	return err
}

const requestFrom = ` FROM requests r
 LEFT JOIN members target ON target.id = r.member_id
 JOIN members requester ON requester.id = r.requested_by `
const requestColumns = `r.id, r.team_id, r.kind, r.member_id, target.name, r.requested_by,
 requester.name, r.args, r.status, r.error_code, r.error_message, r.error_details, r.created_at, r.started_at, r.finished_at`

func scanRequest(row scanner) (*model.Request, error) {
	var r model.Request
	var memberID, targetName, args, errorCode, errorMessage, errorDetails sql.NullString
	var requestedByID string
	var created string
	var started, finished sql.NullString
	err := row.Scan(&r.ID, &r.Team, &r.Kind, &memberID, &targetName, &requestedByID, &r.RequestedBy.Name,
		&args, &r.Status, &errorCode, &errorMessage, &errorDetails, &created, &started, &finished)
	if err != nil {
		return nil, err
	}
	r.RequestedBy.ID = requestedByID
	if memberID.Valid {
		r.Member = &model.MemberRef{ID: memberID.String, Name: targetName.String}
	}
	if err := json.Unmarshal([]byte(args.String), &r.Args); err != nil {
		return nil, asError(err)
	}
	if r.Args == nil {
		r.Args = map[string]any{}
	}
	if errorCode.Valid {
		r.Error = &model.Error{Code: model.ErrorCode(errorCode.String), Message: errorMessage.String, Details: map[string]any{}}
		if err := json.Unmarshal([]byte(errorDetails.String), &r.Error.Details); err != nil {
			return nil, asError(err)
		}
	}
	if r.CreatedAt, err = parseTime(created); err != nil {
		return nil, err
	}
	if r.StartedAt, err = timeFromNull(started); err != nil {
		return nil, err
	}
	if r.FinishedAt, err = timeFromNull(finished); err != nil {
		return nil, err
	}
	return &r, nil
}

// GetRequest returns a request by id.
func (tx *Tx) GetRequest(id string) (*model.Request, error) {
	r, err := scanRequest(tx.queryRow("SELECT "+requestColumns+requestFrom+"WHERE r.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("request")
	}
	return r, asError(err)
}

// ListOpenRequests returns pending and running requests in creation order.
func (tx *Tx) ListOpenRequests(teamID string) ([]model.Request, error) {
	rows, err := tx.query("SELECT "+requestColumns+requestFrom+"WHERE r.team_id = ? AND r.status IN ('pending', 'running') ORDER BY r.created_at, r.id", teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []model.Request
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, asError(err)
		}
		result = append(result, *r)
	}
	return result, asError(rows.Err())
}

// UpdateRequest applies patch only from the expected status.
func (tx *Tx) UpdateRequest(id string, expected model.RequestStatus, patch RequestPatch) error {
	var sets []string
	var args []any
	add := func(column string, value any) { sets = append(sets, column+" = ?"); args = append(args, value) }
	add("status", patch.Status)
	if patch.Error != nil {
		var code, message any
		details := map[string]any{}
		if *patch.Error != nil {
			code, message = (*patch.Error).Code, (*patch.Error).Message
			if (*patch.Error).Details != nil {
				details = (*patch.Error).Details
			}
		}
		encoded, err := json.Marshal(details)
		if err != nil {
			return asError(err)
		}
		add("error_code", code)
		add("error_message", message)
		add("error_details", string(encoded))
	}
	if patch.StartedAt != nil {
		add("started_at", dbTimePtr(*patch.StartedAt))
	}
	if patch.FinishedAt != nil {
		add("finished_at", dbTimePtr(*patch.FinishedAt))
	}
	args = append(args, id, expected)
	result, err := tx.exec("UPDATE requests SET "+strings.Join(sets, ", ")+" WHERE id = ? AND status = ?", args...)
	if err != nil {
		return err
	}
	return requireRows(result, conflict("request is unknown or its status changed"))
}
