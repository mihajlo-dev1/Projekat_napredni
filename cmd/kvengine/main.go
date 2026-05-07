package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"kv-engine/internal/config"
	"kv-engine/internal/engine"
)

func main() {
	// Ako korisnik ne prosledi putanju, koristi se podrazumevani config.
	configPath := "config.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	kv, err := engine.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "engine error: %v\n", err)
		os.Exit(1)
	}
	defer kv.Close()

	if err := kv.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "replay error: %v\n", err)
		os.Exit(1)
	}

	// Jednostavan REPL za rucno testiranje engine-a.
	fmt.Println("KV Engine CLI started")
	fmt.Println("Commands: PUT key value | GET key | DELETE key | MERKLE tableNumber | TABLES | EXIT")

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if shouldExit(line) {
			break
		}

		if err := execute(kv, line); err != nil {
			fmt.Println("ERR", err)
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "input error: %v\n", err)
	}
}

func shouldExit(line string) bool {
	command := strings.ToUpper(strings.Fields(line)[0])
	return command == "EXIT" || command == "QUIT"
}

// execute parsira jednu CLI komandu i poziva odgovarajucu engine metodu.
func execute(kv *engine.Engine, line string) error {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return nil
	}

	switch strings.ToUpper(parts[0]) {
	case "PUT":
		if len(parts) < 3 {
			return errors.New("usage: PUT key value")
		}
		key := parts[1]
		// Vrednost se uzima iz originalne linije da bi smela da sadrzi razmake.
		valueStart := strings.Index(line, key) + len(key)
		value := strings.TrimSpace(line[valueStart:])
		if value == "" {
			return errors.New("usage: PUT key value")
		}
		if err := kv.Put(key, []byte(value)); err != nil {
			return err
		}
		fmt.Println("OK")
	case "GET":
		if len(parts) != 2 {
			return errors.New("usage: GET key")
		}
		value, ok, err := kv.GetWithError(parts[1])
		if err != nil {
			return err
		}
		if !ok {
			fmt.Println("NOT_FOUND")
			return nil
		}
		fmt.Println(string(value))
	case "DELETE":
		if len(parts) != 2 {
			return errors.New("usage: DELETE key")
		}
		if err := kv.Delete(parts[1]); err != nil {
			return err
		}
		fmt.Println("OK")
	case "MERKLE":
		if len(parts) != 2 {
			return errors.New("usage: MERKLE tableNumber")
		}
		tableNumber, err := parsePositiveInt(parts[1])
		if err != nil {
			return errors.New("usage: MERKLE tableNumber")
		}
		valid, changed, err := kv.ValidateMerkle(tableNumber)
		if err != nil {
			return err
		}
		if valid {
			fmt.Println("VALID")
			return nil
		}
		fmt.Println("INVALID", changed)
	case "TABLES":
		if len(parts) != 1 {
			return errors.New("usage: TABLES")
		}
		fmt.Println(kv.TableCount())
	default:
		return fmt.Errorf("unknown command %q", parts[0])
	}

	return nil
}

// parsePositiveInt je mali parser za MERKLE broj tabele.
func parsePositiveInt(value string) (int, error) {
	number := 0
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return 0, errors.New("not a number")
		}
		number = number*10 + int(ch-'0')
	}
	if number < 1 {
		return 0, errors.New("not positive")
	}
	return number, nil
}
