// Command sixmax plays six-handed fixed-limit 2-7 triple draw over the arena
// JSON-lines protocol.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/nuttakit/2-7-bot/internal/sixmax"
	"github.com/nuttakit/2-7-bot/internal/sixmaxclone"
	"github.com/nuttakit/2-7-bot/internal/sixmaxrange"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

var (
	valueAggression = "false"
	earlyTenBreak   = "false"
	rangeCalls      = "false"
	clonePredraw    = "false"
	loadCloneModel  = sixmaxclone.Embedded
)

func main() {
	debug := io.Discard
	if os.Getenv("BOT_DEBUG") != "" {
		debug = os.Stderr
	}
	if err := run(os.Stdin, os.Stdout, debug); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "sixmax: %v\n", err)
		os.Exit(1)
	}
}

func run(input io.Reader, output io.Writer, debug io.Writer) error {
	bot, err := configuredBot()
	if err != nil {
		return err
	}
	replies := bufio.NewWriter(output)
	lines := bufio.NewScanner(input)
	lines.Buffer(make([]byte, 0, 4096), wire.MaxLineBytes)
	for lines.Scan() {
		if len(lines.Bytes()) == 0 {
			continue
		}
		msg, err := wire.DecodeMessage(lines.Bytes())
		if err != nil {
			_, _ = fmt.Fprintf(debug, "undecodable line: %v\n", err)
			continue
		}
		switch msg.Type {
		case wire.MsgHello:
			if msg.GameID != "27td-fl" {
				return fmt.Errorf("unsupported game %q", msg.GameID)
			}
			if msg.SeatCount != sixmax.SeatCount {
				return fmt.Errorf("six seats required, got %d", msg.SeatCount)
			}
			bot.Hello(msg)
			if err := send(replies, wire.Join()); err != nil {
				return err
			}
		case wire.MsgHandStart:
			bot.HandStart(msg)
		case wire.MsgEvent:
			bot.Observe(msg.Event)
		case wire.MsgAct:
			action := bot.Decide(msg.Decision)
			if err := send(replies, wire.Reply(action)); err != nil {
				return err
			}
		case wire.MsgMatchEnd:
			return nil
		}
	}
	return lines.Err()
}

func configuredBot() (*sixmax.Bot, error) {
	config := sixmax.DefaultConfig()
	var err error
	if config.ValueAggression, err = strconv.ParseBool(valueAggression); err != nil {
		return nil, fmt.Errorf("value aggression: %w", err)
	}
	if config.EarlyTenBreak, err = strconv.ParseBool(earlyTenBreak); err != nil {
		return nil, fmt.Errorf("early ten break: %w", err)
	}
	enableRanges, err := strconv.ParseBool(rangeCalls)
	if err != nil {
		return nil, fmt.Errorf("range calls: %w", err)
	}
	bot := sixmax.New(config)
	if enableRanges {
		ranges, loadErr := sixmaxrange.Embedded()
		if loadErr != nil {
			return nil, fmt.Errorf("embedded range model: %w", loadErr)
		}
		bot.Ranges = ranges
	}
	enableClone, err := strconv.ParseBool(clonePredraw)
	if err != nil {
		return nil, fmt.Errorf("predraw clone: %w", err)
	}
	if enableClone {
		clone, loadErr := loadCloneModel()
		if loadErr != nil {
			return nil, fmt.Errorf("embedded predraw clone: %w", loadErr)
		}
		bot.Clone = clone
	}
	return bot, nil
}

func send(output *bufio.Writer, msg wire.BotMsg) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("encode reply: %w", err)
	}
	if _, err := output.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write reply: %w", err)
	}
	if err := output.Flush(); err != nil {
		return fmt.Errorf("flush reply: %w", err)
	}
	return nil
}
