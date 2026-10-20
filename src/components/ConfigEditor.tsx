import React from 'react';
import { InlineField, Input, SecretInput, InlineSwitch, FieldSet, InlineFieldRow } from '@grafana/ui';
import { DataSourcePluginOptionsEditorProps } from '@grafana/data';
import { FirebirdDataSourceOptions, FirebirdSecureJsonData } from '../types';

interface Props extends DataSourcePluginOptionsEditorProps<FirebirdDataSourceOptions, FirebirdSecureJsonData> {}

export function ConfigEditor(props: Props) {
  const { onOptionsChange, options } = props;
  const { jsonData, secureJsonFields, secureJsonData } = options;

  const onHostChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({ ...options, jsonData: { ...jsonData, host: event.target.value } });
  };

  const onPortChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    const port = parseInt(event.target.value, 10) || 3050;
    onOptionsChange({ ...options, jsonData: { ...jsonData, port } });
  };

  const onDatabaseChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({ ...options, jsonData: { ...jsonData, database: event.target.value } });
  };

  const onUserChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({ ...options, jsonData: { ...jsonData, user: event.target.value } });
  };

  const onPasswordChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({ ...options, secureJsonData: { ...secureJsonData, password: event.target.value } });
  };

  const onResetPassword = () => {
    onOptionsChange({
      ...options,
      secureJsonFields: { ...secureJsonFields, password: false },
      secureJsonData: { ...secureJsonData, password: '' },
    });
  };

  const onMaxOpenConnsChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({ ...options, jsonData: { ...jsonData, maxOpenConns: parseInt(event.target.value, 10) || undefined } });
  };

  const onMaxIdleConnsChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({ ...options, jsonData: { ...jsonData, maxIdleConns: parseInt(event.target.value, 10) || undefined } });
  };

  const onConnMaxLifetimeChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({ ...options, jsonData: { ...jsonData, connMaxLifetimeSec: parseInt(event.target.value, 10) || undefined } });
  };

  const onTimeoutChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({ ...options, jsonData: { ...jsonData, timeoutSec: parseInt(event.target.value, 10) || undefined } });
  };

  const onMaxRowsChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({ ...options, jsonData: { ...jsonData, maxRows: parseInt(event.target.value, 10) || undefined } });
  };

  return (
    <FieldSet label="Firebird connection">
      <InlineFieldRow>
        <InlineField label="Host" labelWidth={20} tooltip="Firebird server hostname or IP">
          <Input value={jsonData.host ?? ''} onChange={onHostChange} placeholder="firebird.example.com" width={28} />
        </InlineField>
        <InlineField label="Port" labelWidth={8}>
          <Input value={jsonData.port ?? 3050} onChange={onPortChange} placeholder="3050" width={10} />
        </InlineField>
      </InlineFieldRow>
      <InlineField label="Database path" labelWidth={20} tooltip="Path to the .fdb file as seen by the Firebird server">
        <Input value={jsonData.database ?? ''} onChange={onDatabaseChange} placeholder="D:/data/example.fdb" width={40} />
      </InlineField>
      <InlineField label="User" labelWidth={20} tooltip="Read-only user recommended">
        <Input value={jsonData.user ?? ''} onChange={onUserChange} width={28} />
      </InlineField>
      <InlineField label="Password" labelWidth={20}>
        <SecretInput
          value={secureJsonData?.password ?? ''}
          isConfigured={(secureJsonFields.password ?? false)}
          onChange={onPasswordChange}
          onReset={onResetPassword}
          width={28}
        />
      </InlineField>
      <InlineFieldRow>
        <InlineField label="Max conns" labelWidth={20} tooltip="Max open connections. Keep low: Firebird Classic spawns a process per connection">
          <Input value={jsonData.maxOpenConns ?? ''} onChange={onMaxOpenConnsChange} placeholder="5" width={12} />
        </InlineField>
        <InlineField label="Max idle" labelWidth={16} tooltip="Idle pool size (default 2)">
          <Input value={jsonData.maxIdleConns ?? ''} onChange={onMaxIdleConnsChange} placeholder="2" width={12} />
        </InlineField>
        <InlineField label="Lifetime (s)" labelWidth={20} tooltip="Recycle connections after this many seconds (default 300)">
          <Input value={jsonData.connMaxLifetimeSec ?? ''} onChange={onConnMaxLifetimeChange} placeholder="300" width={12} />
        </InlineField>
      </InlineFieldRow>
      <InlineFieldRow>
        <InlineField label="Timeout (s)" labelWidth={20} tooltip="Per-query timeout in seconds (default 30)">
          <Input value={jsonData.timeoutSec ?? ''} onChange={onTimeoutChange} placeholder="30" width={12} />
        </InlineField>
        <InlineField label="Max rows" labelWidth={16} tooltip="Row cap per query, protects Grafana from unbounded SELECTs (default 100000)">
          <Input value={jsonData.maxRows ?? ''} onChange={onMaxRowsChange} placeholder="100000" width={14} />
        </InlineField>
      </InlineFieldRow>
      <InlineField label="Read-only" labelWidth={14} tooltip="Enforces SELECT/WITH-only statements in the backend (defense in depth: use a read-only DB user too)">
        <InlineSwitch value={true} disabled={true} />
      </InlineField>
    </FieldSet>
  );
}
