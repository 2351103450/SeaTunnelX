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

const HAS_CHINESE_PATTERN = /[\u4e00-\u9fff]/;

const REPLACERS: Array<{
  pattern: RegExp;
  replace: (...args: string[]) => string;
}> = [
  {
    pattern: /^Diagnostic bundle created from error group #(\d+)$/,
    replace: (id) => `错误组 #${id} 触发的诊断包 / Diagnostic bundle created from error group #${id}`,
  },
  {
    pattern: /^Diagnostic bundle created from inspection finding #(\d+)$/,
    replace: (id) => `巡检发现 #${id} 触发的诊断包 / Diagnostic bundle created from inspection finding #${id}`,
  },
  {
    pattern: /^Diagnostic bundle created from alert (.+)$/,
    replace: (id) => `告警 ${id} 触发的诊断包 / Diagnostic bundle created from alert ${id}`,
  },
  {
    pattern: /^Manual diagnostic bundle task$/,
    replace: () => '手动创建的诊断包任务 / Manual diagnostic bundle task',
  },
  {
    pattern: /^Step completed\.$/,
    replace: () => '步骤执行完成。 / Step completed.',
  },
  {
    pattern: /^Diagnostic task completed\.$/,
    replace: () => '诊断任务执行完成。 / Diagnostic task completed.',
  },
  {
    pattern: /^Thread dump is disabled by task options\.$/,
    replace: () => '任务配置未开启线程栈采集。 / Thread dump is disabled by task options.',
  },
  {
    pattern: /^JVM dump is disabled by task options\.$/,
    replace: () => '任务配置未开启 JVM Dump 采集。 / JVM dump is disabled by task options.',
  },
  {
    pattern: /^Thread dump collected\.$/,
    replace: () => '线程栈采集完成。 / Thread dump collected.',
  },
  {
    pattern: /^Log sample collected\.$/,
    replace: () => '日志样本采集完成。 / Log sample collected.',
  },
  {
    pattern: /^No log sample collected\.$/,
    replace: () => '未采集到日志样本。 / No log sample collected.',
  },
  {
    pattern: /^Thread dump collected to (.+)$/,
    replace: (path) => `线程栈已保存到 ${path} / Thread dump collected to ${path}`,
  },
  {
    pattern: /^Collected log sample from (.+)$/,
    replace: (path) => `已从 ${path} 采集日志样本 / Collected log sample from ${path}`,
  },
  {
    pattern: /^Failed to collect log sample from (.+): (.+)$/,
    replace: (path, detail) => `从 ${path} 采集日志样本失败：${detail} / Failed to collect log sample from ${path}: ${detail}`,
  },
  {
    pattern: /^Thread dump failed: (.+)$/,
    replace: (detail) => `线程栈采集失败：${detail} / Thread dump failed: ${detail}`,
  },
  {
    pattern: /^thread dump failed for all nodes: (.+)$/i,
    replace: (detail) =>
      `全部节点线程栈采集失败：${detail} / Thread dump failed on all nodes: ${detail}`,
  },
  {
    pattern: /^JVM dump failed: (.+)$/,
    replace: (detail) => `JVM Dump 采集失败：${detail} / JVM dump failed: ${detail}`,
  },
  {
    pattern: /^jvm dump failed for all nodes: (.+)$/i,
    replace: (detail) =>
      `全部节点 JVM Dump 采集失败：${detail} / JVM dump failed on all nodes: ${detail}`,
  },
  {
    pattern: /^no log samples collected: (.+)$/i,
    replace: (detail) =>
      `全部节点都未采集到日志样本：${detail} / No log samples collected: ${detail}`,
  },
  {
    pattern: /^Created (.+)$/,
    replace: (name) => `已生成 ${name} / Created ${name}`,
  },
  {
    pattern: /^(.+) inspection generated (\d+) findings \((\d+) critical \/ (\d+) warning \/ (\d+) info\)$/,
    replace: (cluster, total, critical, warning, info) =>
      `${cluster} 巡检生成 ${total} 条发现（严重 ${critical} / 告警 ${warning} / 信息 ${info}） / ${cluster} inspection generated ${total} findings (${critical} critical / ${warning} warning / ${info} info)`,
  },
];

export function localizeDiagnosticsText(value?: string | null): string {
  const text = value?.trim() || '';
  if (!text) {
    return '';
  }
  if (HAS_CHINESE_PATTERN.test(text) || text.includes(' / ')) {
    return text;
  }

  for (const item of REPLACERS) {
    const match = text.match(item.pattern);
    if (!match) {
      continue;
    }
    return item.replace(...match.slice(1));
  }

  return text;
}
