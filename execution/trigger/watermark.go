package trigger

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/pkg/errors"

	"github.com/cube2222/octosql"
	"github.com/cube2222/octosql/execution"
	"github.com/cube2222/octosql/streaming/storage"
)

var watermarkPrefix = []byte("$watermark$")

type WatermarkTrigger struct {
}

func NewWatermarkTrigger() *WatermarkTrigger {
	return &WatermarkTrigger{}
}

func (wt *WatermarkTrigger) RecordReceived(ctx context.Context, tx storage.StateTransaction, key octosql.Value, eventTime time.Time) error {
	timeKeys := NewTimeSortedKeys(tx.WithPrefix(timeSortedKeys))
	watermarkStorage := storage.NewValueState(tx.WithPrefix(watermarkPrefix))

	var octoWatermark octosql.Value
	var watermark time.Time
	err := watermarkStorage.Get(&octoWatermark)
	if err == nil {
		watermark = octoWatermark.AsTime()
	} else if err != storage.ErrNotFound {
		return errors.Wrap(err, "couldn't get current watermark")
	}

	if watermark.After(eventTime) {
		// TODO: Handling late data
		log.Printf("late data...? watermark: %v key: %v event_time: %v", watermark, key.Show(), eventTime)
		return nil
	}

	err = timeKeys.Update(key, eventTime)
	if err != nil {
		return errors.Wrap(err, "couldn't update trigger time for key")
	}

	return nil
}

var readyToFirePrefix = []byte(fmt.Sprint("$ready_to_fire$"))

func (wt *WatermarkTrigger) UpdateWatermark(ctx context.Context, tx storage.StateTransaction, watermark time.Time) error {
	watermarkStorage := storage.NewValueState(tx.WithPrefix(watermarkPrefix))
	readyToFire := storage.NewValueState(tx.WithPrefix(readyToFirePrefix))

	octoWatermark := octosql.MakeTime(watermark)
	err := watermarkStorage.Set(&octoWatermark)
	if err != nil {
		return errors.Wrap(err, "couldn't set new watermark value")
	}

	ready, err := wt.isSomethingReadyToFire(ctx, tx, watermark)
	if err != nil {
		return errors.Wrap(err, "couldn't check if something is ready to fire")
	}
	if ready {
		octoReady := octosql.MakeBool(true)
		err := readyToFire.Set(&octoReady)
		if err != nil {
			return errors.Wrap(err, "couldn't set ready to fire")
		}
	}

	return nil
}

func (wt *WatermarkTrigger) isSomethingReadyToFire(ctx context.Context, tx storage.StateTransaction, watermark time.Time) (bool, error) {
	timeKeys := NewTimeSortedKeys(tx.WithPrefix(timeSortedKeys))

	_, sendTime, err := timeKeys.GetFirst()
	if err != nil {
		if err == storage.ErrNotFound {
			return false, nil
		}
		return false, errors.Wrap(err, "couldn't get first key by time")
	}

	if watermark.Before(sendTime) {
		return false, nil
	}

	return true, nil
}

func (wt *WatermarkTrigger) PollKeyToFire(ctx context.Context, tx storage.StateTransaction) (octosql.Value, error) {
	timeKeys := NewTimeSortedKeys(tx.WithPrefix(timeSortedKeys))
	watermarkStorage := storage.NewValueState(tx.WithPrefix(watermarkPrefix))
	readyToFire := storage.NewValueState(tx.WithPrefix(readyToFirePrefix))

	var octoReady octosql.Value
	err := readyToFire.Get(&octoReady)
	if err == storage.ErrNotFound {
		octoReady = octosql.MakeBool(false)
	} else if err != nil {
		return octosql.ZeroValue(), errors.Wrap(err, "couldn't get readiness to fire value")
	}
	if !octoReady.AsBool() {
		return octosql.ZeroValue(), execution.ErrNoKeyToFire
	}

	key, sendTime, err := timeKeys.GetFirst()
	if err != nil {
		if err == storage.ErrNotFound {
			panic("unreachable")
		}
		return octosql.ZeroValue(), errors.Wrap(err, "couldn't get first key by time")
	}

	var octoWatermark octosql.Value
	var watermark time.Time
	err = watermarkStorage.Get(&octoWatermark)
	if err == nil {
		watermark = octoWatermark.AsTime()
	} else if err != storage.ErrNotFound {
		return octosql.ZeroValue(), errors.Wrap(err, "couldn't get current watermark")
	}

	if watermark.Before(sendTime) {
		panic("unreachable")
	}

	err = timeKeys.Delete(key, sendTime)
	if err != nil {
		return octosql.ZeroValue(), errors.Wrap(err, "couldn't delete key")
	}

	ready, err := wt.isSomethingReadyToFire(ctx, tx, watermark)
	if err != nil {
		return octosql.ZeroValue(), errors.Wrap(err, "couldn't check if something is ready to fire")
	}
	if !ready {
		octoReady := octosql.MakeBool(false)
		err := readyToFire.Set(&octoReady)
		if err != nil {
			return octosql.ZeroValue(), errors.Wrap(err, "couldn't set ready to fire")
		}
	}

	return key, nil
}

func (wt *WatermarkTrigger) KeyFired(ctx context.Context, tx storage.StateTransaction, key octosql.Value) error {
	// We don't want to clear the watermark trigger.
	// Keys should always be triggered when the watermark surpasses their event time.
	return nil
}