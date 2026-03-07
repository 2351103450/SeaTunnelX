/**
 * Cluster Detail Page
 * 集群详情页面
 */

import {Suspense} from 'react';
import {ClusterDetail} from '@/components/common/cluster';
import {Metadata} from 'next';

export const metadata: Metadata = {
  title: '集群详情',
};

interface ClusterDetailPageProps {
  params: Promise<{id: string}>;
}

export default async function ClusterDetailPage({params}: ClusterDetailPageProps) {
  const {id} = await params;
  const clusterId = parseInt(id, 10);

  return (
    <Suspense>
      <ClusterDetail clusterId={clusterId} />
    </Suspense>
  );
}
