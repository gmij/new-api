/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useCallback, useEffect, useRef, useState } from 'react';
import {
  Modal,
  Button,
  Typography,
  Spin,
  Tag,
  Descriptions,
  Collapse,
} from '@douyinfe/semi-ui';
import { API, showError } from '../../../../helpers';

const { Text } = Typography;

const formatDurationSeconds = (seconds, t) => {
  const tt = typeof t === 'function' ? t : (v) => v;
  const s = Number(seconds);
  if (!Number.isFinite(s) || s <= 0) return '-';
  const total = Math.floor(s);
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const secs = total % 60;
  if (hours > 0) return `${hours}${tt('小时')} ${minutes}${tt('分钟')}`;
  if (minutes > 0) return `${minutes}${tt('分钟')} ${secs}${tt('秒')}`;
  return `${secs}${tt('秒')}`;
};

const formatExpiresAt = (expiresAt) => {
  if (!expiresAt) return '-';
  try {
    return new Date(expiresAt).toLocaleString();
  } catch {
    return String(expiresAt);
  }
};

const getStatusTag = (status, t) => {
  const tt = typeof t === 'function' ? t : (v) => v;
  switch (status) {
    case 'active':
      return <Tag color='green'>{tt('正常')}</Tag>;
    case 'expiring_soon':
      return <Tag color='amber'>{tt('即将过期')}</Tag>;
    case 'expired':
      return <Tag color='red'>{tt('已过期')}</Tag>;
    default:
      return <Tag color='grey'>{tt('未知')}</Tag>;
  }
};

const getAuthMethodLabel = (authMethod) => {
  switch (authMethod) {
    case 'social':
      return 'Social (Kiro Desktop)';
    case 'IdC':
      return 'IAM Identity Center (IdC)';
    default:
      return authMethod || '-';
  }
};

const KiroInfoView = ({ t, record, payload, onCopy, onRefresh }) => {
  const tt = typeof t === 'function' ? t : (v) => v;
  const [showRawJson, setShowRawJson] = useState(false);
  const data = payload?.data ?? null;
  const errorMessage =
    payload?.success === false
      ? (data?.refresh_error || payload?.message || tt('获取信息失败'))
      : '';

  const status = data?.status ?? 'unknown';
  const authMethod = data?.auth_method ?? '';
  const region = data?.region ?? '';
  const expiresAt = data?.expires_at ?? '';
  const remainingSeconds = data?.remaining_seconds;
  const hasRefreshToken = data?.has_refresh_token ?? false;
  const autoRefreshed = data?.auto_refreshed ?? false;
  const refreshError = data?.refresh_error ?? '';

  const rawText = JSON.stringify(data ?? payload, null, 2);

  return (
    <div className='flex flex-col gap-4'>
      {errorMessage && (
        <div className='rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700'>
          {errorMessage}
        </div>
      )}

      {autoRefreshed && (
        <div className='rounded-xl border border-green-200 bg-green-50 px-4 py-3 text-sm text-green-700'>
          {tt('令牌已自动刷新')}
        </div>
      )}

      <div className='rounded-xl border border-semi-color-border bg-semi-color-bg-0 p-3'>
        <div className='flex flex-wrap items-start justify-between gap-2'>
          <div className='min-w-0'>
            <div className='text-xs font-medium text-semi-color-text-2'>
              {tt('Kiro 凭证状态')}
            </div>
            <div className='mt-2 flex flex-wrap items-center gap-2'>
              {getStatusTag(status, tt)}
              <Tag color='blue' type='light' shape='circle'>
                {region || '-'}
              </Tag>
              <Tag color='grey' type='light' shape='circle'>
                {getAuthMethodLabel(authMethod)}
              </Tag>
            </div>
          </div>
          <Button
            size='small'
            type='tertiary'
            theme='outline'
            onClick={onRefresh}
          >
            {tt('刷新')}
          </Button>
        </div>

        <div className='mt-2 rounded-lg bg-semi-color-fill-0 px-3 py-2'>
          <Descriptions>
            <Descriptions.Item itemKey={tt('令牌过期时间')}>
              <div className='text-xs leading-5 text-semi-color-text-1'>
                {formatExpiresAt(expiresAt)}
              </div>
            </Descriptions.Item>
            <Descriptions.Item itemKey={tt('剩余时间')}>
              <div className='text-xs leading-5 text-semi-color-text-1'>
                {remainingSeconds != null
                  ? formatDurationSeconds(remainingSeconds, tt)
                  : '-'}
              </div>
            </Descriptions.Item>
            <Descriptions.Item itemKey={tt('Refresh Token')}>
              <div className='text-xs leading-5 text-semi-color-text-1'>
                {hasRefreshToken ? (
                  <Tag color='green' size='small'>
                    {tt('已配置')}
                  </Tag>
                ) : (
                  <Tag color='amber' size='small'>
                    {tt('未配置')}
                  </Tag>
                )}
              </div>
            </Descriptions.Item>
            <Descriptions.Item itemKey={tt('认证方式')}>
              <div className='text-xs leading-5 text-semi-color-text-1'>
                {getAuthMethodLabel(authMethod)}
              </div>
            </Descriptions.Item>
            <Descriptions.Item itemKey={tt('区域')}>
              <div className='text-xs leading-5 text-semi-color-text-1'>
                {region || '-'}
              </div>
            </Descriptions.Item>
          </Descriptions>
        </div>

        {refreshError && (
          <div className='mt-2 rounded-lg bg-red-50 px-3 py-2 text-xs text-red-600'>
            {tt('自动刷新失败：')}{refreshError}
          </div>
        )}

        <div className='mt-2 text-xs text-semi-color-text-2'>
          {tt('渠道：')}
          {record?.name || '-'} ({tt('编号：')}
          {record?.id || '-'})
        </div>
      </div>

      <Collapse
        activeKey={showRawJson ? ['raw-json'] : []}
        onChange={(activeKey) => {
          const keys = Array.isArray(activeKey) ? activeKey : [activeKey];
          setShowRawJson(keys.includes('raw-json'));
        }}
      >
        <Collapse.Panel header={tt('原始 JSON')} itemKey='raw-json'>
          <div className='mb-2 flex justify-end'>
            <Button
              size='small'
              type='primary'
              theme='outline'
              onClick={() => onCopy?.(rawText)}
              disabled={!rawText}
            >
              {tt('复制')}
            </Button>
          </div>
          <pre className='max-h-[50vh] overflow-y-auto rounded-lg bg-semi-color-fill-0 p-3 text-xs text-semi-color-text-0'>
            {rawText}
          </pre>
        </Collapse.Panel>
      </Collapse>
    </div>
  );
};

const KiroInfoLoader = ({ t, record, onCopy }) => {
  const tt = typeof t === 'function' ? t : (v) => v;
  const [loading, setLoading] = useState(true);
  const [payload, setPayload] = useState(null);
  const hasShownErrorRef = useRef(false);
  const mountedRef = useRef(true);
  const recordId = record?.id;

  const fetchInfo = useCallback(async () => {
    if (!recordId) {
      if (mountedRef.current) setPayload(null);
      return;
    }

    if (mountedRef.current) setLoading(true);
    try {
      const res = await API.get(`/api/channel/${recordId}/kiro/info`, {
        skipErrorHandler: true,
      });
      if (!mountedRef.current) return;
      setPayload(res?.data ?? null);
      if (!res?.data?.success && !hasShownErrorRef.current) {
        hasShownErrorRef.current = true;
        showError(tt('获取信息失败'));
      }
    } catch (error) {
      if (!mountedRef.current) return;
      if (!hasShownErrorRef.current) {
        hasShownErrorRef.current = true;
        showError(tt('获取信息失败'));
      }
      setPayload({ success: false, message: String(error) });
    } finally {
      if (mountedRef.current) setLoading(false);
    }
  }, [recordId, tt]);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  useEffect(() => {
    fetchInfo().catch(() => {});
  }, [fetchInfo]);

  if (loading) {
    return (
      <div className='flex items-center justify-center py-10'>
        <Spin spinning={true} size='large' tip={tt('加载中...')} />
      </div>
    );
  }

  if (!payload) {
    return (
      <div className='flex flex-col gap-3'>
        <Text type='danger'>{tt('获取信息失败')}</Text>
        <div className='flex justify-end'>
          <Button
            size='small'
            type='primary'
            theme='outline'
            onClick={fetchInfo}
          >
            {tt('刷新')}
          </Button>
        </div>
      </div>
    );
  }

  return (
    <KiroInfoView
      t={tt}
      record={record}
      payload={payload}
      onCopy={onCopy}
      onRefresh={fetchInfo}
    />
  );
};

export const openKiroInfoModal = ({ t, record, onCopy }) => {
  const tt = typeof t === 'function' ? t : (v) => v;

  Modal.info({
    title: tt('Kiro 凭证信息'),
    centered: true,
    width: 700,
    style: { maxWidth: '95vw' },
    content: (
      <KiroInfoLoader
        t={tt}
        record={record}
        onCopy={onCopy}
      />
    ),
    footer: (
      <div className='flex justify-end gap-2'>
        <Button type='primary' theme='solid' onClick={() => Modal.destroyAll()}>
          {tt('关闭')}
        </Button>
      </div>
    ),
  });
};
