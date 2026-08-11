package metrics

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

type Metrics struct {
	ZelloConnected       atomic.Int64
	ConversationActive   atomic.Int64
	RXTransmissionsTotal atomic.Uint64
	TXTransmissionsTotal atomic.Uint64
	WakeDetectionsTotal  atomic.Uint64
	AzureSessionsTotal   atomic.Uint64
	ErrorsTotal          atomic.Uint64
}

func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprintf(w, "mira_zello_connected %d\n", m.ZelloConnected.Load())
		fmt.Fprintf(w, "mira_conversation_active %d\n", m.ConversationActive.Load())
		fmt.Fprintf(w, "mira_rx_transmissions_total %d\n", m.RXTransmissionsTotal.Load())
		fmt.Fprintf(w, "mira_tx_transmissions_total %d\n", m.TXTransmissionsTotal.Load())
		fmt.Fprintf(w, "mira_wakeword_detections_total %d\n", m.WakeDetectionsTotal.Load())
		fmt.Fprintf(w, "mira_azure_sessions_total %d\n", m.AzureSessionsTotal.Load())
		fmt.Fprintf(w, "mira_errors_total %d\n", m.ErrorsTotal.Load())
	})
}
