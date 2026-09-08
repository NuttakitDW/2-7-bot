// Command switsim plays a fitted opponent model over the wire, so a local
// engine can spar our bot against a stand-in for a hosted rival.
//
//	poker-arena run --bot candidate@cmd:./bin/bot --bot swit@cmd:./bin/switsim
//
// SWITSIM_MODEL names the empirical JSON (optionally gzipped); SWITSIM_MODE
// set to anything makes the model play its most likely action instead of
// sampling. This is a local tool, never an upload.
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/lapis"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "switsim: %v\n", err)
		os.Exit(1)
	}
}

func loadModel() (cfr.Model, error) {
	path := os.Getenv("SWITSIM_MODEL")
	if path == "" {
		return nil, fmt.Errorf("SWITSIM_MODEL is not set")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		r, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		if raw, err = io.ReadAll(r); err != nil {
			return nil, err
		}
	}
	m, err := cfr.DecodeEmpirical(raw)
	if err != nil {
		return nil, err
	}
	if os.Getenv("SWITSIM_MODE") != "" {
		return m.Mode(), nil
	}
	return m, nil
}

func run(input io.Reader, output io.Writer) error {
	model, err := loadModel()
	if err != nil {
		return err
	}
	bot := lapis.NewModel(model)
	replies := bufio.NewWriter(output)
	lines := bufio.NewScanner(input)
	lines.Buffer(make([]byte, 0, 4096), wire.MaxLineBytes)
	for lines.Scan() {
		line := lines.Bytes()
		if len(line) == 0 {
			continue
		}
		msg, err := wire.DecodeMessage(line)
		if err != nil {
			continue
		}
		switch msg.Type {
		case wire.MsgHello:
			bot.Hello(msg)
			if err := send(replies, wire.Join()); err != nil {
				return err
			}
		case wire.MsgHandStart:
			bot.HandStart(msg)
		case wire.MsgEvent:
			bot.Observe(msg.Event)
		case wire.MsgAct:
			if err := send(replies, wire.Reply(bot.Decide(msg.Decision))); err != nil {
				return err
			}
		case wire.MsgMatchEnd:
			report(bot)
			return nil
		}
	}
	report(bot)
	return lines.Err()
}

// report appends the fallback count to SWITSIM_LOG, when set; the engine
// does not relay a bot's stderr.
func report(bot *lapis.Bot) {
	path := os.Getenv("SWITSIM_LOG")
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "switsim: %d fallbacks\n", bot.Fallbacks)
}

func send(replies *bufio.Writer, msg wire.BotMsg) error {
	encoded, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if _, err := replies.Write(append(encoded, '\n')); err != nil {
		return err
	}
	return replies.Flush()
}
