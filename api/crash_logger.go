package api

import (
	"encoding/json"

	gethLog "github.com/onflow/go-ethereum/log"
)

const timeFormat = "2006-01-02T15:04:05-07Z"
const errorKey = "LOG15_ERROR"

// JSONFormat formats log records as JSON objects. If pretty is true,
// records will be pretty-printed. If lineSeparated is true, records
// will be logged with a new line between each record.
func JSONFormat(pretty, lineSeparated bool) gethLog.Format {
	jsonMarshal := json.Marshal
	if pretty {
		jsonMarshal = func(v interface{}) ([]byte, error) {
			return json.MarshalIndent(v, "", "    ")
		}
	}

	return gethLog.FormatFunc(func(r *gethLog.Record) []byte {
		if r.Lvl != gethLog.LvlError {
			return nil
		}
		props := map[string]interface{}{
			"time":      r.Time.Format(timeFormat),
			"level":     lvlToString(r.Lvl),
			"message":   r.Msg,
			"component": "API",
		}

		b, err := jsonMarshal(props)
		if err != nil {
			b, _ = jsonMarshal(map[string]string{
				errorKey: err.Error(),
			})
			return b
		}

		if lineSeparated {
			b = append(b, '\n')
		}

		return b
	})
}

// lvlToString returns the name of a Lvl.
func lvlToString(l gethLog.Lvl) string {
	switch l {
	case gethLog.LvlTrace:
		return "trace"
	case gethLog.LvlDebug:
		return "debug"
	case gethLog.LvlInfo:
		return "info"
	case gethLog.LvlWarn:
		return "warn"
	case gethLog.LvlError:
		return "error"
	case gethLog.LvlCrit:
		return "critical"
	default:
		panic("bad level")
	}
}
