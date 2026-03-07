/**
 * Cluster Management Page
 * 集群管理页面
 */

import {Suspense} from 'react';
import {ClusterMain} from '@/components/common/cluster';
import {Metadata} from 'next';

export const metadata: Metadata = {
  title: '集群管理',
};

export default function ClustersPage() {
  return (
    <Suspense>
      <ClusterMain />
    </Suspense>
  );
}
