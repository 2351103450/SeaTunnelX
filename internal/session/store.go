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

// Package session 提供内存会话存储抽象层。
package session

import (
	"context"
	"errors"
	"sync"
	"time"
)

// 错误定义
var (
	ErrKeyNotFound = errors.New("session: key not found")
	ErrExpired     = errors.New("session: key expired")
)

// SessionStore 会话存储接口。
type SessionStore interface {
	Get(ctx context.Context, key string) (any, error)
	Set(ctx context.Context, key string, value any, expiration time.Duration) error
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
}

// memoryItem 内存存储项，包含值和过期时间。
type memoryItem struct {
	value      any
	expiration int64 // Unix 纳秒时间戳，0 表示永不过期
}

func (item *memoryItem) isExpired() bool {
	if item.expiration == 0 {
		return false
	}
	return time.Now().UnixNano() > item.expiration
}

// MemoryStore 内存会话存储实现。
type MemoryStore struct {
	data sync.Map
}

// NewMemoryStore 创建新的内存存储实例。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

// Get 从内存中获取指定 key 的值。
func (m *MemoryStore) Get(ctx context.Context, key string) (any, error) {
	value, ok := m.data.Load(key)
	if !ok {
		return nil, ErrKeyNotFound
	}

	item, ok := value.(*memoryItem)
	if !ok {
		return nil, ErrKeyNotFound
	}

	if item.isExpired() {
		m.data.Delete(key)
		return nil, ErrExpired
	}

	return item.value, nil
}

// Set 将值存储到内存中。
func (m *MemoryStore) Set(ctx context.Context, key string, value any, expiration time.Duration) error {
	var exp int64
	if expiration > 0 {
		exp = time.Now().Add(expiration).UnixNano()
	}

	m.data.Store(key, &memoryItem{value: value, expiration: exp})
	return nil
}

// Delete 从内存中删除指定 key。
func (m *MemoryStore) Delete(ctx context.Context, key string) error {
	m.data.Delete(key)
	return nil
}

// Exists 检查 key 是否存在于内存中。
func (m *MemoryStore) Exists(ctx context.Context, key string) (bool, error) {
	value, ok := m.data.Load(key)
	if !ok {
		return false, nil
	}

	item, ok := value.(*memoryItem)
	if !ok {
		return false, nil
	}

	if item.isExpired() {
		m.data.Delete(key)
		return false, nil
	}

	return true, nil
}
