'use client';

import {ManagementBar} from '@/components/common/layout/ManagementBar';
import {AIAssistant} from '@/components/common/assistant';
import {memo} from 'react';

const MemoizedManagementBar = memo(ManagementBar);

export default function ProjectLayout({children}: {children: React.ReactNode}) {
  return (
    <div className='min-h-screen flex flex-col'>
      <MemoizedManagementBar />
      <div className='flex flex-1 flex-col'>
        <div className='@container/main flex flex-1 flex-col gap-2'>
          {/* Main content container: keep all pages consistent width/padding */}
          <div className='flex flex-col gap-4 mb-8 w-full max-w-none px-3 sm:px-4 lg:px-6 xl:px-8 2xl:px-10 py-6 md:gap-6'>
            {children}
          </div>
        </div>
      </div>
      {/* 全局 AI 问答助手 / Global AI Assistant */}
      <AIAssistant />
    </div>
  );
}
