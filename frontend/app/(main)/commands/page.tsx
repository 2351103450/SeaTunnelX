/**
 * Command Log Page
 * 命令记录页面
 */

import {Suspense} from 'react';
import {CommandMain} from '@/components/common/audit';
import {Metadata} from 'next';

export const metadata: Metadata = {
  title: '命令记录',
};

export default function CommandsPage() {
  return (
    <Suspense>
      <CommandMain />
    </Suspense>
  );
}
