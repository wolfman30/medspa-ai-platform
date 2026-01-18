// AWS Cognito configuration
// These values should match your deployed Cognito User Pool
export const cognitoConfig = {
  userPoolId: import.meta.env.VITE_COGNITO_USER_POOL_ID || '',
  userPoolClientId: import.meta.env.VITE_COGNITO_CLIENT_ID || '',
  region: import.meta.env.VITE_COGNITO_REGION || 'us-east-1',
  // OAuth domain for federated sign-in (Google, etc.)
  oauthDomain: import.meta.env.VITE_COGNITO_DOMAIN || 'medspa-dashboard.auth.us-east-1.amazoncognito.com',
};

// Get the redirect URL based on current environment
export function getRedirectUrl(): string {
  if (typeof window !== 'undefined') {
    return window.location.origin + '/';
  }
  return 'http://localhost:5173/';
}

// Check if Cognito is configured
export const isCognitoConfigured = () => {
  return cognitoConfig.userPoolId !== '' && cognitoConfig.userPoolClientId !== '';
};
