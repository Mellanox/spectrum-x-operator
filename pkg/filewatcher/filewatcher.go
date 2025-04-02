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
	"os"
	"time"
)

// WatchFile watches a file for changes and calls the notify function
// whenever the file is modified. The function runs in a goroutine and can be stopped
// by closing the done channel.
func WatchFile(filepath string, notify func(), done <-chan struct{}) {
	go func() {
		var lastModTime time.Time
		// Get initial modification time
		if info, err := os.Stat(filepath); err == nil {
			lastModTime = info.ModTime()
		}

		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				info, err := os.Stat(filepath)
				if err != nil {
					if os.IsNotExist(err) && !lastModTime.IsZero() {
						lastModTime = time.Time{}
						notify()
						continue
					}
					continue
				}

				if info.ModTime().After(lastModTime) {
					lastModTime = info.ModTime()
					notify()
				}

				select {
				case <-done:
					return
				default:
					continue
				}
			}
		}
	}()
}
