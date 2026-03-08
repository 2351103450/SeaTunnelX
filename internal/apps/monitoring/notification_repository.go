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

package monitoring

import "context"

// ListNotificationRoutes returns all notification routes.
// ListNotificationRoutes 返回全部通知路由。
func (r *Repository) ListNotificationRoutes(ctx context.Context) ([]*NotificationRoute, error) {
	var routes []*NotificationRoute
	if err := r.db.WithContext(ctx).
		Order("id DESC").
		Find(&routes).Error; err != nil {
		return nil, err
	}
	return routes, nil
}

// GetNotificationRouteByID returns one notification route by ID.
// GetNotificationRouteByID 根据 ID 返回单条通知路由。
func (r *Repository) GetNotificationRouteByID(ctx context.Context, id uint) (*NotificationRoute, error) {
	var route NotificationRoute
	if err := r.db.WithContext(ctx).First(&route, id).Error; err != nil {
		return nil, err
	}
	return &route, nil
}

// CreateNotificationRoute creates one notification route.
// CreateNotificationRoute 创建一条通知路由。
func (r *Repository) CreateNotificationRoute(ctx context.Context, route *NotificationRoute) error {
	return r.db.WithContext(ctx).Create(route).Error
}

// SaveNotificationRoute updates one notification route.
// SaveNotificationRoute 更新一条通知路由。
func (r *Repository) SaveNotificationRoute(ctx context.Context, route *NotificationRoute) error {
	return r.db.WithContext(ctx).Save(route).Error
}

// DeleteNotificationRoute deletes one notification route by ID.
// DeleteNotificationRoute 根据 ID 删除一条通知路由。
func (r *Repository) DeleteNotificationRoute(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&NotificationRoute{}, id).Error
}

// CreateNotificationDelivery creates one delivery record.
// CreateNotificationDelivery 创建一条通知投递记录。
func (r *Repository) CreateNotificationDelivery(ctx context.Context, delivery *NotificationDelivery) error {
	return r.db.WithContext(ctx).Create(delivery).Error
}

// SaveNotificationDelivery updates one delivery record.
// SaveNotificationDelivery 更新一条通知投递记录。
func (r *Repository) SaveNotificationDelivery(ctx context.Context, delivery *NotificationDelivery) error {
	return r.db.WithContext(ctx).Save(delivery).Error
}
