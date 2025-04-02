/*
Copyright 2025, NVIDIA CORPORATION & AFFILIATES

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package filewatcher

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "filewatcher suite")
}

var _ = Describe("FileWatcher", func() {
	var (
		tempDir    string
		testFile   string
		notifyChan chan struct{}
		doneChan   chan struct{}
	)

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "filewatcher-test")
		Expect(err).NotTo(HaveOccurred())
		testFile = filepath.Join(tempDir, "test.txt")

		// Create test file
		err = os.WriteFile(testFile, []byte("initial content"), 0644)
		Expect(err).NotTo(HaveOccurred())

		notifyChan = make(chan struct{}, 1)
		doneChan = make(chan struct{})
	})

	AfterEach(func() {
		os.RemoveAll(tempDir)
	})

	notify := func() {
		notifyChan <- struct{}{}
	}

	It("should detect file modifications and send notifications", func() {
		// Start watching the file
		WatchFile(testFile, notify, doneChan)

		// Wait a bit to ensure the watcher is running
		time.Sleep(200 * time.Millisecond)

		// Modify the file
		err := os.WriteFile(testFile, []byte("modified content"), 0644)
		Expect(err).NotTo(HaveOccurred())

		// Wait for notification
		Eventually(notifyChan).Should(Receive())

		// Clean up
		close(doneChan)
	})

	It("should detect file modifications and send notifications multiple times", func() {
		// Start watching the file
		WatchFile(testFile, notify, doneChan)

		// Wait a bit to ensure the watcher is running
		time.Sleep(200 * time.Millisecond)

		// Modify the file multiple times
		for i := 0; i < 5; i++ {
			err := os.WriteFile(testFile, []byte(fmt.Sprintf("modified content %d", i)), 0644)
			Expect(err).NotTo(HaveOccurred())

			// Wait for notification
			Eventually(notifyChan).Should(Receive())
		}

		// Clean up
		close(doneChan)
	})

	It("should stop watching when done channel is closed", func() {
		// Start watching the file
		WatchFile(testFile, notify, doneChan)

		// Wait a bit to ensure the watcher is running
		time.Sleep(200 * time.Millisecond)

		// Close the done channel
		close(doneChan)

		// Wait a bit to ensure the watcher has stopped
		time.Sleep(200 * time.Millisecond)

		// Modify the file
		err := os.WriteFile(testFile, []byte("modified content"), 0644)
		Expect(err).NotTo(HaveOccurred())

		// Verify no notification is received
		Consistently(notifyChan).ShouldNot(Receive())
	})

	It("should handle non-existent files gracefully", func() {
		nonExistentFile := filepath.Join(tempDir, "nonexistent.txt")
		WatchFile(nonExistentFile, notify, doneChan)

		// Wait a bit to ensure the watcher is running
		time.Sleep(200 * time.Millisecond)

		// Create the file
		err := os.WriteFile(nonExistentFile, []byte("new content"), 0644)
		Expect(err).NotTo(HaveOccurred())

		// Wait for notification
		Eventually(notifyChan).Should(Receive())

		// Clean up
		close(doneChan)
	})

	It("should handle file deletions", func() {
		// Start watching the file
		WatchFile(testFile, notify, doneChan)

		// Wait a bit to ensure the watcher is running
		time.Sleep(200 * time.Millisecond)

		// Delete the file
		err := os.Remove(testFile)
		Expect(err).NotTo(HaveOccurred())

		// Wait for notification
		Eventually(notifyChan).Should(Receive())

		// create file again
		err = os.WriteFile(testFile, []byte("modified content"), 0644)
		Expect(err).NotTo(HaveOccurred())

		// Wait for notification
		Eventually(notifyChan).Should(Receive())

		// Clean up
		close(doneChan)
	})
})
