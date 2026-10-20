import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import React from 'react';
import { ConfigEditor } from './ConfigEditor';
import { FirebirdDataSourceOptions, FirebirdSecureJsonData } from '../types';

const setup = () => {
  const options: any = {
    jsonData: {
      host: '',
      port: 3050,
      database: '',
      user: '',
      dialect: 3,
      charset: 'UTF8',
      maxOpenConns: 5,
      maxIdleConns: 2,
      connMaxLifetimeSec: 300,
      timeoutSec: 30,
    } as FirebirdDataSourceOptions,
    secureJsonData: {} as FirebirdSecureJsonData,
    secureJsonFields: {},
  };
  const onOptionsChange = jest.fn();
  render(<ConfigEditor options={options} onOptionsChange={onOptionsChange} />);
  return { options, onOptionsChange };
};

// Grafana InlineField does not associate its label with the input via
// for=/aria-labelledby, so we address inputs via placeholder/role.
const inputByPlaceholder = (re: RegExp) =>
  screen.getAllByRole('textbox').find((el) => re.test(el.getAttribute('placeholder') ?? ''));

describe('ConfigEditor', () => {
  it('renders connection fields', () => {
    setup();
    expect(screen.getByText('Host')).toBeInTheDocument();
    expect(screen.getByText('Database path')).toBeInTheDocument();
    expect(screen.getByText('Password')).toBeInTheDocument();
  });

  it('emits changed host via onOptionsChange', async () => {
    const { onOptionsChange } = setup();
    const host = inputByPlaceholder(/firebird\.example/);
    expect(host).toBeTruthy();
    fireEvent.change(host!, { target: { value: '10.0.0.5' } });
    await waitFor(() => expect(onOptionsChange).toHaveBeenCalled());
    const call = onOptionsChange.mock.calls[0][0];
    expect(call.jsonData.host).toBe('10.0.0.5');
  });

  it('password field writes into secureJsonData, not jsonData', async () => {
    const { onOptionsChange } = setup();
    // SecretInput renders type=password
    const pw = document.querySelector('input[type="password"]') as HTMLInputElement;
    expect(pw).toBeTruthy();
    fireEvent.change(pw, { target: { value: 'secret' } });
    await waitFor(() => expect(onOptionsChange).toHaveBeenCalled());
    const call = onOptionsChange.mock.calls[0][0];
    expect(call.secureJsonData.password).toBe('secret');
    expect(call.jsonData.password).toBeUndefined();
  });
});
