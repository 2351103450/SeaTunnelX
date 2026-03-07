/**
 * Audit Log Page
 * 审计日志页面
 */

import {Suspense} from 'react';
import {AuditLogMain} from '@/components/common/audit';
import {Metadata} from 'next';

export const metadata: Metadata = {
  title: '审计日志',
};

export default function AuditLogsPage() {
  return (
    <Suspense>
      <AuditLogMain />
    </Suspense>
  );
}
