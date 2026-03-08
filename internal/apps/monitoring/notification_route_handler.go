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

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// TestNotificationChannel handles POST /api/v1/monitoring/notification-channels/:id/test
// TestNotificationChannel 处理通知渠道测试发送接口。
func (h *Handler) TestNotificationChannel(c *gin.Context) {
	channelID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, Response{ErrorMsg: "invalid channel id"})
		return
	}

	data, err := h.service.TestNotificationChannel(c.Request.Context(), uint(channelID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, Response{ErrorMsg: "Failed to test notification channel: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, Response{Data: data})
}

// ListNotificationRoutes handles GET /api/v1/monitoring/notification-routes
// ListNotificationRoutes 处理通知路由列表接口。
func (h *Handler) ListNotificationRoutes(c *gin.Context) {
	data, err := h.service.ListNotificationRoutes(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, Response{ErrorMsg: "Failed to list notification routes: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, Response{Data: data})
}

// CreateNotificationRoute handles POST /api/v1/monitoring/notification-routes
// CreateNotificationRoute 处理新增通知路由接口。
func (h *Handler) CreateNotificationRoute(c *gin.Context) {
	var req UpsertNotificationRouteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, Response{ErrorMsg: "invalid request body: " + err.Error()})
		return
	}
	if err := req.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, Response{ErrorMsg: err.Error()})
		return
	}

	data, err := h.service.CreateNotificationRoute(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, Response{ErrorMsg: "Failed to create notification route: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, Response{Data: data})
}

// UpdateNotificationRoute handles PUT /api/v1/monitoring/notification-routes/:id
// UpdateNotificationRoute 处理更新通知路由接口。
func (h *Handler) UpdateNotificationRoute(c *gin.Context) {
	routeID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, Response{ErrorMsg: "invalid route id"})
		return
	}

	var req UpsertNotificationRouteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, Response{ErrorMsg: "invalid request body: " + err.Error()})
		return
	}
	if err := req.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, Response{ErrorMsg: err.Error()})
		return
	}

	data, err := h.service.UpdateNotificationRoute(c.Request.Context(), uint(routeID), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, Response{ErrorMsg: "Failed to update notification route: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, Response{Data: data})
}

// DeleteNotificationRoute handles DELETE /api/v1/monitoring/notification-routes/:id
// DeleteNotificationRoute 处理删除通知路由接口。
func (h *Handler) DeleteNotificationRoute(c *gin.Context) {
	routeID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, Response{ErrorMsg: "invalid route id"})
		return
	}

	if err := h.service.DeleteNotificationRoute(c.Request.Context(), uint(routeID)); err != nil {
		c.JSON(http.StatusInternalServerError, Response{ErrorMsg: "Failed to delete notification route: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, Response{Data: gin.H{"id": routeID}})
}
