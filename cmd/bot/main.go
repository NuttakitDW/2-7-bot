// Command bot plays 27td-fl on the MixedSolver Arena.
//
// The hosted platform spawns this as a subprocess and speaks JSON Lines over
// its stdin and stdout; there is no address argument and no socket to open
// (docs/arena/hosted-bot-interface.md). stderr is free for diagnostics and is
// not part of the protocol.
//
// Build for upload with:
//
//	make bot-release
//
// The artifact filename is the bot name (docs/naming.md).
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/nuttakit/2-7-bot/internal/onyx"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

// debugEnv turns on stderr diagnostics. Off by default: a real match runs
// hundreds of thousands of hands and a chatty bot is a slow one.
const debugEnv = "BOT_DEBUG"

func main() {
	debug := io.Discard
	if os.Getenv(debugEnv) != "" {
		debug = os.Stderr
	}
	if err := run(os.Stdin, os.Stdout, debug); err != nil {
		fmt.Fprintf(os.Stderr, "bot: %v\n", err)
		os.Exit(1)
	}
}

// run reads arena messages until match-end or EOF.
func run(input io.Reader, output io.Writer, debug io.Writer) error {
	bot, err := onyx.New()
	if err != nil {
		return err
	}
	replies := bufio.NewWriter(output)

	lines := bufio.NewScanner(input)
	lines.Buffer(make([]byte, 0, 4096), wire.MaxLineBytes)

	for lines.Scan() {
		line := lines.Bytes()
		if len(line) == 0 {
			continue // blank lines are ignored by readers
		}
		msg, err := wire.DecodeMessage(line)
		if err != nil {
			// A line we cannot parse is the arena's problem, not a
			// reason to abandon the match: staying in costs nothing and
			// leaving forfeits every remaining hand.
			_, _ = fmt.Fprintf(debug, "undecodable line: %v\n", err)
			continue
		}

		switch msg.Type {
		case wire.MsgHello:
			bot.Hello(msg)
			_, _ = fmt.Fprintf(debug, "hello: %s, %d seats, timeout %dms\n",
				msg.GameID, msg.SeatCount, bot.Table.Match.TimeoutMs)
			if err := send(replies, wire.Join()); err != nil {
				return err
			}

		case wire.MsgHandStart:
			bot.HandStart(msg)

		case wire.MsgEvent:
			bot.Observe(msg.Event)

		case wire.MsgAct:
			action := bot.Decide(msg.Decision)
			_, _ = fmt.Fprintf(debug, "hand %d street %s: %v -> %s\n",
				bot.Table.Hand.No, bot.Table.Hand.Label, bot.Table.Hand.Cards, action.Kind)
			if err := send(replies, wire.Reply(action)); err != nil {
				return err
			}

		case wire.MsgMatchEnd:
			_, _ = fmt.Fprintln(debug, "match end")
			return nil

		default:
			// joined, hand-end, and any message type a newer arena adds.
			// Unrecognized tags are no-ops by contract, never errors.
		}
	}
	// EOF without a match-end: the arena closed the pipe. Nothing left to
	// play, and nothing to complain about.
	return lines.Err()
}

// send writes one compact JSON line and flushes, because the arena is waiting
// on it and a buffered reply is an unanswered one.
func send(replies *bufio.Writer, msg wire.BotMsg) error {
	encoded, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("encoding %s: %w", msg.Type, err)
	}
	if _, err := replies.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("writing %s: %w", msg.Type, err)
	}
	if err := replies.Flush(); err != nil {
		return fmt.Errorf("flushing %s: %w", msg.Type, err)
	}
	return nil
}
