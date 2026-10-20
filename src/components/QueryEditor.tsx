import React, { ChangeEvent, useMemo } from 'react';
import { InlineField, InlineFieldRow, Select, TextArea } from '@grafana/ui';
import { QueryEditorProps, SelectableValue } from '@grafana/data';
import { FirebirdQuery, FirebirdDataSourceOptions, FirebirdSecureJsonData } from '../types';
import { DataSource } from '../datasource';

type Props = QueryEditorProps<DataSource, FirebirdQuery, FirebirdDataSourceOptions, FirebirdQuery>;

export function QueryEditor({ onChange, onRunQuery, query }: Props) {
  const formatOptions = useMemo<SelectableValue<'table' | 'time_series'>[]>(
    () => [
      { value: 'table', label: 'Table' },
      { value: 'time_series', label: 'Time series' },
    ],
    []
  );

  const onRawSqlChange = (event: ChangeEvent<HTMLTextAreaElement>) => {
    onChange({ ...query, rawSql: event.target.value });
  };

  const onFormatChange = (value: SelectableValue<'table' | 'time_series'>) => {
    onChange({ ...query, format: value.value ?? 'table' });
  };

  const rawSql = query.rawSql ?? '';

  return (
    <>
      <TextArea
        value={rawSql}
        onChange={onRawSqlChange}
        onBlur={onRunQuery}
        rows={8}
        placeholder="SELECT FIRST 100 ORDERNUMBER, ORDERDATE FROM TABLE1 WHERE $__timeFilter(ORDERDATE)"
        spellCheck={false}
      />
      <InlineFieldRow>
        <InlineField label="Format" labelWidth={10}>
          <Select value={query.format ?? 'table'} options={formatOptions} onChange={onFormatChange} width={16} />
        </InlineField>
      </InlineFieldRow>
    </>
  );
}
