// Command zircon plays dedicated six-handed fixed-limit 2-7 triple draw.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/nuttakit/2-7-bot/internal/wire"
	"github.com/nuttakit/2-7-bot/internal/zircon"
)

var botProfile = "generation2"

func main() {
	debug := io.Discard
	if os.Getenv("BOT_DEBUG") != "" {
		debug = os.Stderr
	}
	if err := run(os.Stdin, os.Stdout, debug); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "zircon: %v\n", err)
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
			if msg.SeatCount != zircon.SeatCount {
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
			if err := send(replies, wire.Reply(bot.Decide(msg.Decision))); err != nil {
				return err
			}
		case wire.MsgMatchEnd:
			reportOverrides(debug, bot)
			return nil
		}
	}
	reportOverrides(debug, bot)
	return lines.Err()
}

func reportOverrides(debug io.Writer, bot *zircon.Bot) {
	stats := bot.OverrideStats()
	_, _ = fmt.Fprintf(debug, "predraw overrides=%d matrix=%v\n", stats.Applied, stats.Matrix)
}

func configuredBot() (*zircon.Bot, error) {
	switch botProfile {
	case "baseline":
		return zircon.NewBaseline(), nil
	case "generation2":
		return zircon.NewGeneration2(), nil
	default:
		return nil, fmt.Errorf("unknown zircon profile %q", botProfile)
	}
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
