/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package diagnostics

import (
	"fmt"
	"strings"
)

func bilingualText(zh, en string) string {
	zh = strings.TrimSpace(zh)
	en = strings.TrimSpace(en)
	switch {
	case zh == "":
		return en
	case en == "":
		return zh
	default:
		return zh + " / " + en
	}
}

func resolveDiagnosticCommandFailure(output string, err error, fallbackZH, fallbackEN string) string {
	detail := strings.TrimSpace(output)
	if detail == "" && err != nil {
		detail = strings.TrimSpace(err.Error())
	}
	if detail == "" {
		return bilingualText(fallbackZH, fallbackEN)
	}
	return detail
}

func formatDiagnosticAllNodesFailed(prefixZH, prefixEN string, items []string) error {
	prefix := bilingualText(prefixZH, prefixEN)
	filtered := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			filtered = append(filtered, item)
		}
	}
	if len(filtered) == 0 {
		return fmt.Errorf("%s", prefix)
	}
	return fmt.Errorf("%s: %s", prefix, strings.Join(filtered, "; "))
}
