package index

import (
	"bufio"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestZZOccurrences is a throwaway probe for step S2: it writes every
// occurrence of every file of a game, and a hash of each file's, so two
// builds can be compared. It is deleted when the step is measured.
func TestZZOccurrences(t *testing.T) {
	dir, gameID := os.Getenv("PMT_ZZ_DIR"), os.Getenv("PMT_ZZ_GAME")
	install := os.Getenv("PMT_TEST_INSTALL_" + strings.ToUpper(gameID))
	if dir == "" || install == "" {
		t.Skip()
	}

	_, scanned, _ := scanInstall(t, gameID, install)
	stem := filepath.Join(dir, gameID+"-"+os.Getenv("PMT_ZZ_NAME"))
	all, err := os.Create(stem + ".all")
	if err != nil {
		t.Fatal(err)
	}

	hashes, err := os.Create(stem + ".hash")
	if err != nil {
		t.Fatal(err)
	}

	writer, hashWriter := bufio.NewWriterSize(all, 1<<20), bufio.NewWriter(hashes)
	for position, file := range scanned.Files {
		sum := fnv.New64a()
		for _, occurrence := range scanned.Occurrences[position] {
			line := fmt.Sprintf("%s\t%d\t%v\t%s\t%s\tfiring=%v\n", file.Rel, occurrence.Line+1, occurrence.Role,
				scanned.Databases.Texts[occurrence.Database], scanned.Names.Texts[occurrence.Name], occurrence.Firing)
			sum.Write([]byte(line))
			writer.WriteString(line)
		}

		fmt.Fprintf(hashWriter, "%s\t%x\n", file.Rel, sum.Sum64())
	}

	writer.Flush()
	hashWriter.Flush()
	all.Close()
	hashes.Close()
}
