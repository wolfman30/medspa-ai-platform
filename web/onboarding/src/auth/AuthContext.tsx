import { createContext, useContext, useState, useEffect } from 'react';
import type { ReactNode } from 'react';
import { Amplify } from 'aws-amplify';
import { signIn, signOut, signUp, confirmSignUp, getCurrentUser, fetchAuthSession } from 'aws-amplify/auth';
import { cognitoConfig, isCognitoConfigured } from './config';

// Configure Amplify if Cognito is set up
if (isCognitoConfigured()) {
  Amplify.configure({
    Auth: {
      Cognito: {
        userPoolId: cognitoConfig.userPoolId,
        userPoolClientId: cognitoConfig.userPoolClientId,
      },
    },
  });
}

interface User {
  email: string;
  username: string;
}

interface AuthContextType {
  user: User | null;
  isLoading: boolean;
  isAuthenticated: boolean;
  authEnabled: boolean;
  login: (email: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  register: (email: string, password: string) => Promise<{ needsConfirmation: boolean }>;
  confirmRegistration: (email: string, code: string) => Promise<void>;
  getAccessToken: () => Promise<string | null>;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const authEnabled = isCognitoConfigured();

  useEffect(() => {
    if (!authEnabled) {
      setIsLoading(false);
      return;
    }
    checkUser();
  }, [authEnabled]);

  async function checkUser() {
    try {
      const currentUser = await getCurrentUser();
      setUser({
        email: currentUser.signInDetails?.loginId || currentUser.username,
        username: currentUser.username,
      });
    } catch {
      setUser(null);
    } finally {
      setIsLoading(false);
    }
  }

  async function login(email: string, password: string) {
    const result = await signIn({ username: email, password });
    if (result.isSignedIn) {
      await checkUser();
    }
  }

  async function logout() {
    await signOut();
    setUser(null);
  }

  async function register(email: string, password: string) {
    const result = await signUp({
      username: email,
      password,
      options: {
        userAttributes: { email },
      },
    });
    return { needsConfirmation: !result.isSignUpComplete };
  }

  async function confirmRegistration(email: string, code: string) {
    await confirmSignUp({ username: email, confirmationCode: code });
  }

  async function getAccessToken(): Promise<string | null> {
    if (!authEnabled) return null;
    try {
      const session = await fetchAuthSession();
      return session.tokens?.accessToken?.toString() || null;
    } catch {
      return null;
    }
  }

  return (
    <AuthContext.Provider
      value={{
        user,
        isLoading,
        isAuthenticated: !!user,
        authEnabled,
        login,
        logout,
        register,
        confirmRegistration,
        getAccessToken,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}

// eslint-disable-next-line react-refresh/only-export-components
export function useAuth() {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return context;
}
