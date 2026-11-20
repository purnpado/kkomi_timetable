package timetable

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

const (
	historyCollection = "timetable_history"
	historyDocID      = "stack"
	maxHistoryDepth   = 20
)

type historyStackDoc struct {
	NextSeq int64                            `json:"nextSeq"`
	Undo    []protocol.TimetableHistoryEntry `json:"undo"`
	Redo    []protocol.TimetableHistoryEntry `json:"redo"`
}

func loadHistoryDocTx(tx *database.TxStore) (historyStackDoc, error) {
	docPtr, err := database.LoadRecordTx[historyStackDoc](tx, historyCollection, historyDocID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return historyStackDoc{}, nil
		}
		return historyStackDoc{}, err
	}
	if docPtr == nil {
		return historyStackDoc{}, nil
	}
	return *docPtr, nil
}

func statusFromHistoryDoc(doc historyStackDoc) protocol.TimetableHistoryStatus {
	st := protocol.TimetableHistoryStatus{
		CanUndo:   len(doc.Undo) > 0,
		CanRedo:   len(doc.Redo) > 0,
		UndoCount: len(doc.Undo),
		RedoCount: len(doc.Redo),
	}
	if st.CanUndo {
		st.LastUndo = doc.Undo[len(doc.Undo)-1].Action
	}
	if st.CanRedo {
		st.LastRedo = doc.Redo[len(doc.Redo)-1].Action
	}
	return st
}

// recordHistoryTx는 단일 InnoDB 트랜잭션 내부에서 시간표 편집 전/후 스냅샷을 Undo 스택에 기록합니다.
func recordHistoryTx(tx *database.TxStore, action, actorName string, before, after []protocol.TimetableCell) error {
	doc, err := loadHistoryDocTx(tx)
	if err != nil {
		return err
	}
	doc.NextSeq++
	bCopy := make([]protocol.TimetableCell, len(before))
	copy(bCopy, before)
	aCopy := make([]protocol.TimetableCell, len(after))
	copy(aCopy, after)

	entry := protocol.TimetableHistoryEntry{
		ID:        fmt.Sprintf("hist-%d", doc.NextSeq),
		Seq:       doc.NextSeq,
		Action:    action,
		ActorName: actorName,
		CreatedAt: time.Now().Format("2006-01-02 15:04:05"),
		Before:    bCopy,
		After:     aCopy,
	}
	doc.Undo = append(doc.Undo, entry)
	if len(doc.Undo) > maxHistoryDepth {
		doc.Undo = doc.Undo[len(doc.Undo)-maxHistoryDepth:]
	}
	doc.Redo = nil
	return tx.SaveRecord(historyCollection, historyDocID, doc)
}

// GetHistoryStatus는 현재 시간표 실행취소(Undo)/다시실행(Redo) 가능 상태를 반환합니다.
func (s *Service) GetHistoryStatus() (protocol.TimetableHistoryStatus, error) {
	doc, err := database.LoadRecord[historyStackDoc](s.store, historyCollection, historyDocID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return protocol.TimetableHistoryStatus{}, nil
		}
		return protocol.TimetableHistoryStatus{}, err
	}
	if doc == nil {
		return protocol.TimetableHistoryStatus{}, nil
	}
	return statusFromHistoryDoc(*doc), nil
}

// UndoTimetable은 정규 시간표를 직전 상태(Before)로 원자적 복원하고 Redo 스택에 적재합니다.
func (s *Service) UndoTimetable(actorName string) (protocol.TimetableHistoryStatus, []protocol.TimetableCell, error) {
	var (
		status   protocol.TimetableHistoryStatus
		restored []protocol.TimetableCell
		undone   protocol.TimetableHistoryEntry
	)
	err := s.store.WithTx(func(tx *database.TxStore) error {
		doc, lErr := loadHistoryDocTx(tx)
		if lErr != nil {
			return lErr
		}
		if len(doc.Undo) == 0 {
			return errors.New("실행취소(Undo)할 시간표 편집 내역이 없습니다")
		}
		undone = doc.Undo[len(doc.Undo)-1]
		doc.Undo = doc.Undo[:len(doc.Undo)-1]
		doc.Redo = append(doc.Redo, undone)
		if len(doc.Redo) > maxHistoryDepth {
			doc.Redo = doc.Redo[len(doc.Redo)-maxHistoryDepth:]
		}

		if cErr := tx.ClearCollections("timetable_cells"); cErr != nil {
			return cErr
		}
		for _, c := range undone.Before {
			if sErr := tx.SaveRecord("timetable_cells", c.ID, c); sErr != nil {
				return sErr
			}
		}
		if hErr := tx.SaveRecord(historyCollection, historyDocID, doc); hErr != nil {
			return hErr
		}
		restored = undone.Before
		status = statusFromHistoryDoc(doc)
		return nil
	})
	if err != nil {
		return protocol.TimetableHistoryStatus{}, nil, err
	}
	if s.bus != nil {
		s.bus.Publish(protocol.DomainEvent{
			Type:      protocol.EventTimetableCellsUpdated,
			Title:     fmt.Sprintf("↩ 실행취소: %s", undone.Action),
			Message:   fmt.Sprintf("%d개 수업 셀이 이전 상태로 복원되었습니다.", len(restored)),
			ActorName: actorName,
			Payload:   map[string]interface{}{"mode": "UNDO", "count": len(restored)},
		})
	}
	return status, restored, nil
}

// RedoTimetable은 직전에 실행취소한 시간표 상태(After)로 다시 적용합니다.
func (s *Service) RedoTimetable(actorName string) (protocol.TimetableHistoryStatus, []protocol.TimetableCell, error) {
	var (
		status   protocol.TimetableHistoryStatus
		restored []protocol.TimetableCell
		redone   protocol.TimetableHistoryEntry
	)
	err := s.store.WithTx(func(tx *database.TxStore) error {
		doc, lErr := loadHistoryDocTx(tx)
		if lErr != nil {
			return lErr
		}
		if len(doc.Redo) == 0 {
			return errors.New("다시실행(Redo)할 시간표 편집 내역이 없습니다")
		}
		redone = doc.Redo[len(doc.Redo)-1]
		doc.Redo = doc.Redo[:len(doc.Redo)-1]
		doc.Undo = append(doc.Undo, redone)
		if len(doc.Undo) > maxHistoryDepth {
			doc.Undo = doc.Undo[len(doc.Undo)-maxHistoryDepth:]
		}

		if cErr := tx.ClearCollections("timetable_cells"); cErr != nil {
			return cErr
		}
		for _, c := range redone.After {
			if sErr := tx.SaveRecord("timetable_cells", c.ID, c); sErr != nil {
				return sErr
			}
		}
		if hErr := tx.SaveRecord(historyCollection, historyDocID, doc); hErr != nil {
			return hErr
		}
		restored = redone.After
		status = statusFromHistoryDoc(doc)
		return nil
	})
	if err != nil {
		return protocol.TimetableHistoryStatus{}, nil, err
	}
	if s.bus != nil {
		s.bus.Publish(protocol.DomainEvent{
			Type:      protocol.EventTimetableCellsUpdated,
			Title:     fmt.Sprintf("↪ 다시실행: %s", redone.Action),
			Message:   fmt.Sprintf("%d개 수업 셀이 다시 반영되었습니다.", len(restored)),
			ActorName: actorName,
			Payload:   map[string]interface{}{"mode": "REDO", "count": len(restored)},
		})
	}
	return status, restored, nil
}
