package brokers

import "testing"

func TestChangeEventNamesTheStepAndSkipsNonNews(t *testing.T) {
	open := &Order{ID: "B-1", Status: "OPEN"}
	partial := &Order{ID: "B-1", Status: "PARTIALLY_FILLED", FilledQuantity: 1, AveragePrice: 99.5}
	filled := &Order{ID: "B-1", Status: "FILLED", FilledQuantity: 2}
	row := func(status string, order *Order) Record { return Record{Status: status, Order: order} }

	for name, c := range map[string]struct {
		before Record
		status string
		order  *Order
		detail string
		source string
		kind   string
	}{
		"acknowledged":            {row(StatusPending, nil), StatusSubmitted, open, "", SourceSubmit, EventAcknowledged},
		"acknowledged on a retry": {row(StatusPending, nil), StatusSubmitted, open, "", SourceSubmit, EventAcknowledged},
		"refused":                 {row(StatusPending, nil), StatusFailed, nil, "insufficient funds", SourceSubmit, EventRefused},
		"no answer":               {row(StatusPending, nil), StatusUnknown, nil, "no answer", SourceSubmit, EventNoAnswer},
		"settled":                 {row(StatusUnknown, nil), StatusSubmitted, open, "", SourceReconcile, EventSettled},
		"a stale pending found":   {row(StatusPending, nil), StatusSubmitted, open, "", SourceReconcile, EventSettled},
		"partial fill":            {row(StatusSubmitted, open), StatusSubmitted, partial, "", SourceReconcile, EventUpdate},
		"more fills":              {row(StatusSubmitted, partial), StatusSubmitted, filled, "", SourceReconcile, EventUpdate},
		"given up":                {row(StatusUnknown, nil), StatusFailed, nil, "no order after 10 minutes", SourceReconcile, EventGivenUp},
		"pending became unknown":  {row(StatusPending, nil), StatusUnknown, nil, "looking", SourceReconcile, EventNoAnswer},
		"cancel":                  {row(StatusSubmitted, open), StatusSubmitted, &Order{ID: "B-1", Status: "CANCELLED"}, "", SourceCancel, EventCancel},
	} {
		ev, ok := ChangeEvent(c.before, c.status, c.order, c.detail, c.source)
		if !ok || ev.Kind != c.kind || ev.Status != c.status || ev.Detail != c.detail {
			t.Errorf("%s: %+v ok=%v, want kind %s", name, ev, ok, c.kind)
		}
		if c.order != nil && (ev.OrderStatus != c.order.Status || ev.FilledQuantity != c.order.FilledQuantity || ev.AveragePrice != c.order.AveragePrice) {
			t.Errorf("%s: the broker's report is kept: %+v", name, ev)
		}
	}
	for name, c := range map[string]struct {
		before Record
		status string
		order  *Order
	}{
		"the same report again":    {row(StatusSubmitted, partial), StatusSubmitted, &Order{ID: "B-1", Status: "PARTIALLY_FILLED", FilledQuantity: 1}},
		"still unknown":            {row(StatusUnknown, nil), StatusUnknown, nil},
		"a status with no meaning": {row(StatusPending, nil), StatusPending, nil},
	} {
		if ev, ok := ChangeEvent(c.before, c.status, c.order, "", SourceReconcile); ok {
			t.Errorf("%s: recorded %+v", name, ev)
		}
	}
}
