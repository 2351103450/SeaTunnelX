/**
 * Host Management Page
 * 主机管理页面
 */

import {Suspense} from 'react';
import {HostMain} from '@/components/common/host';
import {Metadata} from 'next';

export const metadata: Metadata = {
  title: '主机管理',
};

export default function HostsPage() {
  return (
    <Suspense>
      <HostMain />
    </Suspense>
  );
}
