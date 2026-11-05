package service

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
	"strings"
	"time"
)

const metadataWorkflowTTL = 10 * time.Minute
const metadataWorkflowCapacity = 512

type metadataWorkflowReceipt struct {
	ProfileID   int64
	Input       domain.VideoMetadataPreviewInput
	Fingerprint string
	ExpiresAt   time.Time
}

func (s *Service) metadataWorkflowProfile(profileID int64) error {
	if profileID <= 0 {
		return store.ErrMetadataArguments
	}
	if _, err := s.store.ProfileByID(profileID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.ErrMetadataProfile
		}
		return err
	}
	return nil
}

// Search enqueues provider work. It never selects a result automatically.
func (s *Service) SearchVideoMetadataWorkflow(profileID, videoID int64, query, kind string) (domain.VideoMetadataJob, error) {
	if err := s.metadataWorkflowProfile(profileID); err != nil {
		return domain.VideoMetadataJob{}, err
	}
	query = strings.TrimSpace(query)
	if videoID <= 0 || query == "" || len(query) > 500 || kind != "movie" && kind != "series" {
		return domain.VideoMetadataJob{}, store.ErrMetadataArguments
	}
	job, err := s.QueueVideoMetadata(videoID, query, kind)
	if err != nil {
		return domain.VideoMetadataJob{}, err
	}
	if job.Query != query || job.Kind != kind {
		return domain.VideoMetadataJob{}, errors.New("metadata-job-conflict")
	}
	return safeMetadataWorkflowJob(job), nil
}

// Do not call VideoMetadataJob here: its provider-cache purge is a write.
func (s *Service) VideoMetadataWorkflowJob(profileID, videoID, jobID int64) (domain.VideoMetadataJob, error) {
	job, err := s.store.VideoMetadataWorkflowJob(profileID, videoID, jobID)
	if err != nil {
		return domain.VideoMetadataJob{}, err
	}
	return safeMetadataWorkflowJob(job), nil
}

func safeMetadataWorkflowJob(job domain.VideoMetadataJob) domain.VideoMetadataJob {
	switch job.ErrorCode {
	case "", "provider-failed", "provider-request-invalid", "provider-unavailable", "provider-unauthorized", "provider-rate-limited", "provider-response-too-large", "provider-response-invalid", "input-changed", "worker-interrupted":
	default:
		job.ErrorCode = "provider-failed"
	}
	return job
}

func (s *Service) PreviewVideoMetadataWorkflow(profileID int64, in domain.VideoMetadataPreviewInput) (domain.VideoMetadataPreview, error) {
	in = cloneMetadataWorkflowInput(in)
	snapshot, err := s.store.PreviewVideoMetadataWorkflow(profileID, in)
	if err != nil {
		return domain.VideoMetadataPreview{}, err
	}
	var random [32]byte
	if _, err = rand.Read(random[:]); err != nil {
		return domain.VideoMetadataPreview{}, err
	}
	receipt := hex.EncodeToString(random[:])
	now := time.Now()
	s.metadataWorkflowMu.Lock()
	defer s.metadataWorkflowMu.Unlock()
	if s.metadataWorkflowReceipts == nil {
		s.metadataWorkflowReceipts = make(map[string]metadataWorkflowReceipt)
	}
	for key, value := range s.metadataWorkflowReceipts {
		if !now.Before(value.ExpiresAt) {
			delete(s.metadataWorkflowReceipts, key)
		}
	}
	if len(s.metadataWorkflowReceipts) >= metadataWorkflowCapacity {
		return domain.VideoMetadataPreview{}, errors.New("metadata-receipt-capacity")
	}
	expires := now.Add(metadataWorkflowTTL)
	s.metadataWorkflowReceipts[receipt] = metadataWorkflowReceipt{ProfileID: profileID, Input: cloneMetadataWorkflowInput(in), Fingerprint: snapshot.Fingerprint, ExpiresAt: expires}
	snapshot.Preview.Receipt, snapshot.Preview.ExpiresAt = receipt, expires
	return snapshot.Preview, nil
}

func (s *Service) ApplyVideoMetadataWorkflow(profileID int64, in domain.VideoMetadataApplyInput) (domain.VideoMetadataApplyResult, error) {
	if !in.Confirm {
		return domain.VideoMetadataApplyResult{}, errors.New("metadata-confirm-required")
	}
	if err := s.metadataWorkflowProfile(profileID); err != nil {
		return domain.VideoMetadataApplyResult{}, err
	}
	if in.VideoID <= 0 || len(in.Receipt) != 64 {
		return domain.VideoMetadataApplyResult{}, errors.New("metadata-receipt-invalid")
	}
	s.metadataWorkflowMu.Lock()
	defer s.metadataWorkflowMu.Unlock()
	receipt, exists := s.metadataWorkflowReceipts[in.Receipt]
	if !exists || receipt.ProfileID != profileID || receipt.Input.VideoID != in.VideoID {
		return domain.VideoMetadataApplyResult{}, errors.New("metadata-receipt-invalid")
	}
	// A matched receipt is consumed on the first confirmation attempt, including
	// stale or failed attempts. Retrying requires a fresh, reviewable preview.
	delete(s.metadataWorkflowReceipts, in.Receipt)
	if !time.Now().Before(receipt.ExpiresAt) {
		return domain.VideoMetadataApplyResult{}, errors.New("metadata-receipt-expired")
	}
	return s.store.ApplyVideoMetadataWorkflow(profileID, receipt.Input, receipt.Fingerprint, receipt.ExpiresAt)
}

func cloneMetadataWorkflowInput(in domain.VideoMetadataPreviewInput) domain.VideoMetadataPreviewInput {
	if in.Season != nil {
		value := *in.Season
		in.Season = &value
	}
	if in.Episode != nil {
		value := *in.Episode
		in.Episode = &value
	}
	return in
}
