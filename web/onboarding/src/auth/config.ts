// AWS Cognito configuration
// These values should match your deployed Cognito User Pool
export const cognitoConfig = {
  userPoolId: import.meta.env.VITE_COGNITO_USER_POOL_ID || '',
  userPoolClientId: import.meta.env.VITE_COGNITO_CLIENT_ID || '',
  region: import.meta.env.VITE_COGNITO_REGION || 'us-east-1',
};

// Check if Cognito is configured
export const isCognitoConfigured = () => {
  return cognitoConfig.userPoolId !== '' && cognitoConfig.userPoolClientId !== '';
};
