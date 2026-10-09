declare module "*.css";

interface Window {
  runtime?: {
    EventsOn: (eventName: string, callback: (...data: unknown[]) => void) => () => void;
    BrowserOpenURL: (url: string) => void;
  };
}
