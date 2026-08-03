// Tests for web/app/login/page.tsx — Login form.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, cleanup, waitFor } from '@testing-library/react';

// Mock the auth module so we can spy on login() calls without hitting fetch.
vi.mock('../lib/auth', () => ({
  login: vi.fn(() => Promise.resolve()),
}));

import LoginPage from '../app/login/page';
import * as authMod from '../lib/auth';

beforeEach(() => {
  // Stub window.location.assign by replacing location.
  // happy-dom lets us override href assignment via Object.defineProperty.
  Object.defineProperty(window, 'location', {
    configurable: true,
    value: { href: '', assign: vi.fn() } as unknown as Location,
  });
});

afterEach(() => {
  vi.restoreAllMocks();
  cleanup();
});

describe('LoginPage', () => {
  it('renders all fields and the submit button', () => {
    render(<LoginPage />);
    expect(screen.getByTestId('login-form')).toBeInTheDocument();
    expect(screen.getByTestId('input-user')).toBeInTheDocument();
    expect(screen.getByTestId('input-pass')).toBeInTheDocument();
    expect(screen.getByTestId('btn-login')).toBeInTheDocument();
  });

  it('calls auth.login() and redirects on success', async () => {
    const loginSpy = vi.mocked(authMod.login).mockResolvedValueOnce();
    render(<LoginPage />);
    fireEvent.change(screen.getByTestId('input-user'), { target: { value: 'admin' } });
    fireEvent.change(screen.getByTestId('input-pass'), { target: { value: 's3cret' } });
    fireEvent.click(screen.getByTestId('btn-login'));
    await waitFor(() => {
      expect(loginSpy).toHaveBeenCalledWith('admin', 's3cret');
    });
    await waitFor(() => {
      expect(window.location.href).toBe('/');
    });
  });

  it('shows "Invalid credentials" on 401', async () => {
    vi.mocked(authMod.login).mockRejectedValueOnce(new Error('401 Unauthorized'));
    render(<LoginPage />);
    fireEvent.change(screen.getByTestId('input-user'), { target: { value: 'admin' } });
    fireEvent.change(screen.getByTestId('input-pass'), { target: { value: 'wrong' } });
    fireEvent.click(screen.getByTestId('btn-login'));
    await waitFor(() => {
      expect(screen.getByTestId('login-error')).toHaveTextContent(/invalid/i);
    });
  });
});