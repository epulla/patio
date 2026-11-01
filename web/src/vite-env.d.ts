declare module "*.css";
declare module "*.png" {
  const url: string;
  export default url;
}

interface Window {
  runtime?: {
    EventsOn: (eventName: string, callback: (...data: unknown[]) => void) => () => void;
    BrowserOpenURL: (url: string) => void;
  };
}
